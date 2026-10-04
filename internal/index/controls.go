package index

import "strings"

// Control — контрольный вопрос по поваренной книге.
type Control struct {
	Question string
	Search   string
	Expect   string
	Facts    [][]string
	Section  string
}

// Controls — мини-набор из 10 вопросов: что ждать в ответе и какой раздел книги.
func Controls() []Control {
	return []Control{
		{
			Question: "Как сварить кофе в кофейнике?",
			Search:   "boiled coffee",
			Expect:   "1 чашка кофе, 1 яйцо, 6 чашек кипятка, кипятить 3 минуты",
			Facts:    [][]string{{"3 минут", "три минут"}, {"яйц"}, {"6 чаш", "шесть чаш"}},
			Section:  "Boiled Coffee",
		},
		{
			Question: "Как сделать фильтрованный кофе?",
			Search:   "filtered coffee",
			Expect:   "1 чашка мелкого помола и 6 чашек кипятка, лить воду постепенно",
			Facts:    [][]string{{"6 чаш", "шесть чаш"}, {"ситеч", "фильтр"}},
			Section:  "Filtered Coffee",
		},
		{
			Question: "Как заварить чай?",
			Search:   "how to make tea",
			Expect:   "3 чайные ложки чая на 2 чашки кипятка, настаивать 5 минут",
			Facts:    [][]string{{"3 чайн", "три чайн"}, {"5 минут", "пять минут"}},
			Section:  "How to Make Tea",
		},
		{
			Question: "Как приготовить лимонад?",
			Search:   "lemonade",
			Expect:   "1 чашка сахара, треть чашки лимонного сока, пинта воды, сироп 12 минут",
			Facts:    [][]string{{"сахар"}, {"лимон"}, {"12 минут", "двенадцать минут", "двенадцати минут"}},
			Section:  "Lemonade",
		},
		{
			Question: "Как сварить какао к завтраку?",
			Search:   "breakfast cocoa",
			Expect:   "1,5 столовые ложки какао, 2 ложки сахара, 2 чашки воды и 2 чашки молока",
			Facts:    [][]string{{"какао"}, {"молок"}, {"1½", "1,5", "полтор"}},
			Section:  "Breakfast Cocoa",
		},
		{
			Question: "Как сделать горячий шоколад?",
			Search:   "chocolate beverage",
			Expect:   "1,5 квадрата шоколада, четверть чашки сахара, 3 чашки молока",
			Facts:    [][]string{{"шоколад"}, {"молок"}, {"3 стакан", "3 чаш", "три стакан"}},
			Section:  "Chocolate I",
		},
		{
			Question: "Как испечь хлеб на воде?",
			Search:   "water bread",
			Expect:   "2 чашки кипятка, масло, сало, сахар, соль, дрожжи и 6 чашек муки",
			Facts:    [][]string{{"дрожж"}, {"мук"}, {"смальц", "сала"}},
			Section:  "Water Bread",
		},
		{
			Question: "Как приготовить голландский соус?",
			Search:   "hollandaise sauce",
			Expect:   "полчашки масла, желтки 2 яиц, лимонный сок, водяная баня",
			Facts:    [][]string{{"желт"}, {"лимон"}, {"масл"}, {"кайен"}},
			Section:  "Hollandaise Sauce I",
		},
		{
			Question: "Как сделать мятный джулеп?",
			Search:   "mint julep",
			Expect:   "сироп из кварты воды и 2 чашек сахара, кларет, соки и 12 веточек мяты",
			Facts:    [][]string{{"мят"}, {"сахар"}, {"кларет"}},
			Section:  "Mint Julep",
		},
		{
			Question: "Как приготовить имбирный пунш?",
			Search:   "ginger punch",
			Expect:   "кварта воды, чашка сахара, полфунта имбиря, соки апельсина и лимона, варить 15 минут",
			Facts:    [][]string{{"имбир"}, {"15 минут", "пятнадцать минут"}, {"кантон", "полфунт"}},
			Section:  "Ginger Punch",
		},
	}
}

// FactsHit — все группы фактов нашлись в ответе.
func FactsHit(reply string, groups [][]string) bool {
	if strings.TrimSpace(reply) == "" || len(groups) == 0 {
		return false
	}
	low := strings.ToLower(reply)
	for _, group := range groups {
		ok := false
		for _, alt := range group {
			if strings.Contains(low, strings.ToLower(alt)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// SectionHit — среди найденных чанков есть нужный раздел, а не соседний номер.
func SectionHit(hits []Match, section string) bool {
	for _, h := range hits {
		if sectionNameMatches(h.Section, section) {
			return true
		}
	}
	return false
}

func sectionNameMatches(section, want string) bool {
	s := strings.ToLower(strings.TrimSpace(section))
	w := strings.ToLower(strings.TrimSpace(want))
	if s == "" || w == "" {
		return false
	}
	i := strings.Index(s, w)
	if i < 0 {
		return false
	}
	if i > 0 && (i < 2 || s[i-2:i] != ". ") {
		return false
	}
	end := i + len(w)
	if end < len(s) {
		next := s[end]
		if (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') {
			return false
		}
	}
	return true
}
