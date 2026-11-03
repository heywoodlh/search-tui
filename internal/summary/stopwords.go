package summary

var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range []string{
		"a", "about", "above", "after", "again", "against", "all", "also", "am", "an", "and", "any", "are",
		"as", "at", "be", "because", "been", "before", "being", "below", "between", "both", "but", "by",
		"can", "could", "did", "do", "does", "doing", "down", "during", "each", "even", "few", "for", "from",
		"further", "get", "gets", "got", "had", "has", "have", "having", "he", "her", "here", "hers", "him",
		"his", "how", "however", "i", "if", "in", "into", "is", "it", "its", "itself", "just", "let", "like",
		"made", "make", "many", "may", "me", "might", "more", "most", "much", "must", "my", "no", "nor", "not",
		"now", "of", "off", "on", "once", "one", "only", "or", "other", "our", "ours", "out", "over", "own",
		"per", "same", "see", "she", "should", "since", "so", "some", "such", "than", "that", "the", "their",
		"theirs", "them", "then", "there", "these", "they", "this", "those", "through", "to", "too", "under",
		"until", "up", "upon", "us", "use", "used", "using", "very", "via", "was", "we", "well", "were", "what",
		"when", "where", "which", "while", "who", "whom", "why", "will", "with", "within", "without", "would",
		"you", "your", "yours", "yourself", "new", "way", "ways", "two", "first", "last", "www", "com", "http",
		"https", "html", "read", "learn", "click", "page", "site", "website", "official", "find", "help",
		"want", "need", "know", "thing", "things", "lot", "really", "still", "yet", "already", "every",
		"another", "able", "across", "along", "around", "etc", "eg", "ie", "will", "shall", "according",
		"including", "include", "includes", "based", "many", "within", "whether", "though", "although",
	} {
		m[w] = true
	}
	return m
}()
