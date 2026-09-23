package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var jdkDir = regexp.MustCompile(`jdk-(\d+)`)

// FindJava ищет JDK 17+. Сначала явный путь и JAVA_HOME, потом кэш Gradle, потом java из PATH.
func FindJava(explicit string) (string, error) {
	if explicit != "" {
		if err := checkJava(explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		candidate := filepath.Join(home, "bin", "java")
		if err := checkJava(candidate); err == nil {
			return candidate, nil
		}
	}
	if found := newestGradleJDK(); found != "" {
		return found, nil
	}
	if path, err := exec.LookPath("java"); err == nil {
		if err := checkJava(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("не найден JDK 17+: задайте MCP_JAVA или JAVA_HOME")
}

func newestGradleJDK() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(home, ".gradle", "jdks")
	bestPath := ""
	bestMajor := 0
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || info.Name() != "java" {
			return nil
		}
		if !strings.Contains(path, string(filepath.Separator)+"bin"+string(filepath.Separator)+"java") {
			return nil
		}
		major := jdkMajor(path)
		if major < 17 || major < bestMajor {
			return nil
		}
		if err := checkJava(path); err != nil {
			return nil
		}
		bestMajor = major
		bestPath = path
		return nil
	})
	return bestPath
}

func jdkMajor(path string) int {
	m := jdkDir.FindStringSubmatch(path)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func checkJava(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	out, err := exec.Command(path, "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	major := versionMajor(string(out))
	if major < 17 {
		return fmt.Errorf("%s: java %d, нужен 17+", path, major)
	}
	return nil
}

func versionMajor(text string) int {
	// openjdk version "21.0.12" or version "17.0.1"
	i := strings.Index(text, `"`)
	if i < 0 || i+1 >= len(text) {
		return 0
	}
	rest := text[i+1:]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return 0
	}
	ver := rest[:j]
	if strings.HasPrefix(ver, "1.") {
		ver = ver[2:]
	}
	head := ver
	if dot := strings.Index(head, "."); dot >= 0 {
		head = head[:dot]
	}
	n, _ := strconv.Atoi(head)
	return n
}
