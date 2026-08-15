// Package enrich joins the provider data to external datasets.
//
// Everything this package writes is annotation. It never modifies a provider column, and
// every row it writes carries the dataset it came from, the row id inside that dataset,
// and a match confidence. A match below the threshold produces no row at all: an
// unmatched recipe is honest, a wrong match is not.
package enrich

import (
	"regexp"
	"sort"
	"strings"
)

// stopWords are cooking and measurement words that appear in nearly every ingredient
// line. Leaving them in would make every recipe look similar to every other recipe and
// inflate the Jaccard score, which is the failure mode that turns a weak match into a
// confident wrong one.
//
// Words that identify WHICH PART of a plant is eaten -- leaf, seed, flower, stem -- are
// deliberately not here. They read like noise but they are the difference between
// pumpkin and pumpkin leaves, which have different nutrition entirely.
var stopWords = map[string]bool{
	"cup": true, "cups": true, "tablespoon": true, "tablespoons": true, "teaspoon": true,
	"teaspoons": true, "tsp": true, "tbsp": true, "gram": true, "grams": true, "gms": true,
	"kg": true, "ml": true, "litre": true, "liter": true, "inch": true, "pinch": true,
	"to": true, "taste": true, "as": true, "required": true, "needed": true, "or": true,
	"and": true, "of": true, "for": true, "the": true, "a": true, "an": true, "in": true,
	"with": true, "into": true, "chopped": true, "finely": true, "sliced": true,
	"grated": true, "crushed": true, "ground": true, "powder": true, "powdered": true,
	"fresh": true, "dried": true, "raw": true, "whole": true, "half": true, "small": true,
	"large": true, "medium": true, "big": true, "cut": true, "peeled": true, "washed": true,
	"soaked": true, "boiled": true, "cooked": true, "roasted": true, "optional": true,
	"few": true, "some": true, "little": true, "piece": true, "pieces": true, "nos": true,
	"handful": true, "thinly": true, "roughly": true, "deseeded": true, "seedless": true,
	"pcs": true, "no": true, "per": true,
}

// synonyms fold regional and English names of the same food onto one token. Every entry
// is a naming variant of a single ingredient, not a substitution: "atta" and "wheat" are
// the same flour under two names, whereas ragi and bajra are different millets and are
// deliberately absent from this table.
var synonyms = map[string]string{
	"atta": "wheat", "maida": "wheat", "suji": "semolina", "rava": "semolina",
	"sooji": "semolina", "besan": "chickpea", "chana": "chickpea", "channa": "chickpea",
	"kabuli": "chickpea", "moong": "mung", "mung": "mung", "masoor": "lentil",
	"masur": "lentil", "toor": "pigeonpea", "tur": "pigeonpea", "arhar": "pigeonpea",
	"urad": "blackgram", "urid": "blackgram", "rajma": "kidneybean", "lobia": "cowpea",
	"aloo": "potato", "alu": "potato", "batata": "potato", "gajar": "carrot",
	"palak": "spinach", "methi": "fenugreek", "kaddu": "pumpkin", "kumro": "pumpkin",
	"lauki": "bottlegourd", "ghia": "bottlegourd", "doodhi": "bottlegourd",
	"parwal": "pointedgourd", "potol": "pointedgourd", "karela": "bittergourd",
	"tinda": "applegourd", "bhindi": "okra", "baingan": "brinjal", "begun": "brinjal",
	"eggplant": "brinjal", "aubergine": "brinjal", "shalgam": "turnip",
	"phool": "cauliflower", "gobi": "cauliflower", "gobhi": "cauliflower",
	"patta": "cabbage", "matar": "peas", "pea": "peas", "tamatar": "tomato",
	"pyaz": "onion", "pyaaz": "onion", "kanda": "onion", "lehsun": "garlic",
	"adrak": "ginger", "haldi": "turmeric", "jeera": "cumin", "dhania": "coriander",
	"kothmir": "coriander", "cilantro": "coriander", "mirch": "chilli",
	"mirchi": "chilli", "chili": "chilli", "chile": "chilli", "namak": "salt",
	"chawal": "rice", "chaval": "rice", "bhaat": "rice", "chira": "flattenedrice",
	"chire": "flattenedrice", "chiura": "flattenedrice", "poha": "flattenedrice",
	"chivda": "flattenedrice", "murmura": "puffedrice", "muri": "puffedrice",
	"doodh": "milk", "dahi": "curd", "yogurt": "curd", "yoghurt": "curd",
	"curds": "curd", "chhena": "paneer", "chenna": "paneer", "cottage": "paneer",
	"makhan": "butter", "anda": "egg", "murgh": "chicken", "murgi": "chicken",
	"machli": "fish", "machh": "fish", "maach": "fish", "rui": "rohu",
	"gud": "jaggery", "gur": "jaggery", "cheeni": "sugar", "shakkar": "sugar",
	"til": "sesame", "gingelly": "sesame", "moongphali": "peanut",
	"groundnut": "peanut", "badam": "almond", "kaju": "cashew", "kishmish": "raisin",
	"nariyal": "coconut", "narkel": "coconut", "kela": "banana", "aam": "mango",
	"seb": "apple", "papita": "papaya", "amrud": "guava", "nimbu": "lemon",
	"lime": "lemon", "dal": "dal", "daal": "dal", "dhal": "dal", "sag": "greens",
	"saag": "greens", "shak": "greens", "ragi": "fingermillet", "nachni": "fingermillet",
	"bajra": "pearlmillet", "jowar": "sorghum", "jwar": "sorghum",
}

var nonAlpha = regexp.MustCompile(`[^a-z]+`)

// Tokenise turns an ingredient list into a normalised, deduplicated, sorted token set.
// Quantities, units and preparation words are dropped so what remains is the food itself.
func Tokenise(text string) []string {
	seen := map[string]bool{}
	for _, raw := range nonAlpha.Split(strings.ToLower(text), -1) {
		w := strings.TrimSpace(raw)
		if len(w) < 3 || stopWords[w] {
			continue
		}
		w = singular(w)
		if s, ok := synonyms[w]; ok {
			w = s
		}
		if len(w) < 3 || stopWords[w] {
			continue
		}
		seen[w] = true
	}
	out := make([]string, 0, len(seen))
	for w := range seen {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// singular strips the common English plural. It deliberately does not attempt real
// stemming: over-stemming collapses distinct foods onto one token, which is worse than
// missing a match.
func singular(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "oes") && len(w) > 4:
		return w[:len(w)-2]
	case strings.HasSuffix(w, "ss"):
		return w
	case strings.HasSuffix(w, "s") && len(w) > 3:
		return w[:len(w)-1]
	}
	return w
}

// Jaccard is the size of the intersection over the size of the union. Both inputs must
// be sorted and deduplicated, which Tokenise guarantees.
func Jaccard(a, b []string) (float64, []string) {
	if len(a) == 0 || len(b) == 0 {
		return 0, nil
	}
	set := make(map[string]bool, len(a))
	for _, x := range a {
		set[x] = true
	}
	var shared []string
	for _, y := range b {
		if set[y] {
			shared = append(shared, y)
		}
	}
	union := len(a) + len(b) - len(shared)
	if union == 0 {
		return 0, nil
	}
	return float64(len(shared)) / float64(union), shared
}

// Cover is the fraction of a that appears in b -- specifically, how much of the
// PROVIDER's ingredient list the external recipe accounts for. It is deliberately
// asymmetric: dividing by the smaller of the two sets would let a short external recipe
// score 1.0 against a longer provider recipe it only partly covers.
func Cover(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	_, shared := Jaccard(a, b)
	return float64(len(shared)) / float64(len(a))
}
