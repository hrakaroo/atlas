package library

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	searchGroups  = regexp.MustCompile(`"([^"]*)"|([\pL\pN_]+)`)
	searchWords   = regexp.MustCompile(`[\pL\pN_]+`)
	previewMarks  = regexp.MustCompile("[#*\\[\\]`_]")
	explicitBlurb = regexp.MustCompile(`(?mi)^- Blurb:[\s\p{Z}]*([^\n]+)$`)
	blurbHeading  = regexp.MustCompile(`(?mi)^## (?:Summary|Purpose)[\s\p{Z}]*\n`)
	nextHeading   = regexp.MustCompile(`(?m)^## `)
	wikiBlurb     = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	markdownBlurb = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	blurbMarks    = regexp.MustCompile("[*`_]")
)

type searchGroup struct {
	Words  []string
	Phrase bool
}

func truncateRunes(text string, count int) string {
	runes := []rune(text)
	if len(runes) > count {
		return string(runes[:count])
	}
	return text
}

func documentBlurb(summary string) string {
	text := ""
	if match := explicitBlurb.FindStringSubmatch(summary); match != nil {
		text = match[1]
	} else {
		text = summary
		if section := blurbHeading.FindStringIndex(summary); section != nil {
			text = summary[section[1]:]
			if boundary := nextHeading.FindStringIndex(text); boundary != nil {
				text = text[:boundary[0]]
			}
			text = strings.TrimSpace(text)
		}
		selected := ""
		for _, paragraph := range strings.Split(text, "\n\n") {
			paragraph = strings.TrimSpace(paragraph)
			if paragraph != "" && !strings.ContainsRune("#>-|", []rune(paragraph)[0]) {
				selected = paragraph
				break
			}
		}
		text = selected
	}
	text = wikiBlurb.ReplaceAllStringFunc(text, func(match string) string {
		parts := wikiBlurb.FindStringSubmatch(match)
		if parts[2] != "" {
			return parts[2]
		}
		return parts[1]
	})
	text = markdownBlurb.ReplaceAllString(text, "$1")
	text = blurbMarks.ReplaceAllString(text, "")
	return strings.Join(strings.Fields(text), " ")
}

// Listing hydrates indexed lightweight records only. File reconciliation and
// original-reference expansion belong to explicit read/change operations.
func (l *Library) Listing(arguments map[string]any) ([]Record, error) {
	scope := strDefault(arguments, "search_scope", "full_text")
	if scope != "title" && scope != "full_text" {
		return nil, errors.New("Search titles or full text.")
	}
	sortBy := strDefault(arguments, "sort_by", "date")
	if sortBy != "date" && sortBy != "title" && sortBy != "updates" {
		return nil, errors.New("Sort by date, title, or updates.")
	}
	collection := ""
	if value, present := arguments["collection"]; present && value != nil {
		var ok bool
		collection, ok = value.(string)
		if !ok || (collection != "people" && collection != "interviews" && collection != "meetings") {
			return nil, errors.New("Unknown collection.")
		}
	}
	query := ""
	if value, present := arguments["query"]; present {
		var ok bool
		query, ok = value.(string)
		if !ok {
			return nil, errors.New("A search query must be a string.")
		}
	}
	tags, err := stringsArg(arguments["tag_ids"])
	if err != nil {
		return nil, err
	}
	where := []string{"i.deleted_at IS NULL"}
	params := []any{}
	groups := []searchGroup{}
	terms := []string{}
	excerpts := map[string]string{}
	if strings.TrimSpace(query) != "" {
		for _, match := range searchGroups.FindAllStringSubmatch(query, -1) {
			text := match[1]
			if text == "" {
				text = match[2]
			}
			words := searchWords.FindAllString(text, -1)
			if len(words) > 0 {
				groups = append(groups, searchGroup{Words: words, Phrase: match[1] != ""})
				terms = append(terms, words...)
			}
		}
		if len(terms) > 0 {
			expressions := []string{}
			for _, group := range groups {
				expression := "\"" + strings.Join(group.Words, " ") + "\""
				if !group.Phrase {
					expression += "*"
				}
				expressions = append(expressions, expression)
			}
			expression := strings.Join(expressions, " AND ")
			if scope == "title" {
				expression = "title : (" + expression + ")"
			}
			found, e := l.rows("SELECT item_id,snippet(search,-1,'','',' … ',36) AS excerpt FROM search WHERE search MATCH ?", expression)
			if e != nil {
				return nil, e
			}
			for _, row := range found {
				excerpts[str(row, "item_id")] = str(row, "excerpt")
			}
			where = append(where, "(i.id IN (SELECT item_id FROM search WHERE search MATCH ?) OR i.id IN (SELECT m.item_id FROM series_members m JOIN meeting_series s ON s.id=m.series_id WHERE lower(s.title) LIKE ?))")
			params = append(params, expression, "%"+strings.ToLower(strings.TrimSpace(query))+"%")
		}
	}
	for _, tag := range tags {
		entity, e := l.row("SELECT * FROM entities WHERE id=?", tag)
		if e != nil {
			return nil, e
		}
		if entity != nil {
			where = append(where, "(EXISTS (SELECT 1 FROM entity_relationships r WHERE r.item_id=i.id AND r.entity_id=?) OR EXISTS (SELECT 1 FROM meeting_people p WHERE p.item_id=i.id AND p.person_id=?) OR EXISTS (SELECT 1 FROM item_authors a WHERE a.item_id=i.id AND a.normalized_name=?) OR EXISTS(SELECT 1 FROM document_people d WHERE d.item_id=i.id AND d.person_id=?))")
			var person any
			name := ""
			if str(entity, "kind") == "person" {
				person = entity["profile_id"]
				name = caseFold(str(entity, "name"))
			}
			params = append(params, entity["id"], person, name, person)
		} else {
			where = append(where, "EXISTS (SELECT 1 FROM item_tags it WHERE it.item_id=i.id AND it.tag_id=?)")
			params = append(params, tag)
		}
	}
	if truth(arguments["blog_feed_id"]) {
		where = append(where, "EXISTS (SELECT 1 FROM blog_members m WHERE m.item_id=i.id AND m.feed_id=?)")
		params = append(params, arguments["blog_feed_id"])
	}
	if truth(arguments["series_id"]) {
		where = append(where, "EXISTS (SELECT 1 FROM series_members m WHERE m.item_id=i.id AND m.series_id=?)")
		params = append(params, arguments["series_id"])
	}
	if truth(arguments["kind"]) {
		switch str(arguments, "kind") {
		case "document":
			where = append(where, "i.kind IN ('document','reference')")
		case "interview":
			where = append(where, "(i.kind='interview' OR EXISTS (SELECT 1 FROM item_tags it JOIN tags t ON t.id=it.tag_id WHERE it.item_id=i.id AND t.category='type' AND lower(t.name)='interview'))")
		default:
			where = append(where, "i.kind=?")
			params = append(params, arguments["kind"])
		}
	}
	if truth(arguments["starred_only"]) {
		where = append(where, "i.starred=1")
	}
	if truth(arguments["inbox"]) {
		where = append(where, "i.inbox=1")
	}
	found, err := l.rows("SELECT i.id FROM items i WHERE "+strings.Join(where, " AND ")+" ORDER BY COALESCE(i.period_start,NULLIF(i.date,''),substr(i.created,1,10)) DESC, i.title", params...)
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, row := range found {
		item, e := l.Get(str(row, "id"), false, true)
		if e != nil {
			return nil, e
		}
		kind := str(item, "kind")
		if collection == "people" && (kind != "person" || truth(item["is_interview_candidate"])) {
			continue
		}
		if collection == "interviews" && !truth(item["is_interview"]) && !truth(item["is_interview_candidate"]) {
			continue
		}
		if collection == "meetings" && ((kind != "meeting" && kind != "one-on-one") || truth(item["is_interview"])) {
			continue
		}
		summary := str(item, "summary")
		item["preview"] = truncateRunes(strings.ReplaceAll(previewMarks.ReplaceAllString(summary, ""), "\n", " "), 220)
		if kind == "document" || kind == "reference" {
			item["document_blurb"] = documentBlurb(summary)
		}
		if len(terms) > 0 {
			titleWords := searchWords.FindAllString(caseFold(str(item, "title")), -1)
			titleText := " " + strings.Join(titleWords, " ") + " "
			hits := 0
			for _, group := range groups {
				matched := false
				if group.Phrase {
					matched = strings.Contains(titleText, " "+caseFold(strings.Join(group.Words, " "))+" ")
				} else {
					prefix := caseFold(group.Words[0])
					for _, word := range titleWords {
						if strings.HasPrefix(word, prefix) {
							matched = true
							break
						}
					}
				}
				if matched {
					hits++
				}
			}
			rank := 0
			match := "content"
			if hits == len(groups) {
				rank = 2
				match = "title"
			} else if hits > 0 {
				rank = 1
			}
			item["search_title_rank"] = rank
			item["search_match"] = match
			if match == "content" {
				item["search_excerpt"] = excerpts[str(item, "id")]
			}
		}
		delete(item, "summary")
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool { return caseFold(str(result[i], "title")) < caseFold(str(result[j], "title")) })
	if sortBy == "date" || sortBy == "updates" {
		sort.SliceStable(result, func(i, j int) bool { return listingDate(result[i]) > listingDate(result[j]) })
	}
	starsFirst := true
	if value, present := arguments["stars_first"]; present {
		starsFirst = truth(value)
	}
	if starsFirst && sortBy != "updates" {
		sort.SliceStable(result, func(i, j int) bool { return truth(result[i]["starred"]) && !truth(result[j]["starred"]) })
	}
	if sortBy == "updates" {
		if err = l.annotateDocumentChanges(result); err != nil {
			return nil, err
		}
		sort.SliceStable(result, func(i, j int) bool {
			return number(result[i]["latest_change_id"]) > number(result[j]["latest_change_id"])
		})
	}
	if len(terms) > 0 {
		sort.SliceStable(result, func(i, j int) bool {
			return number(result[i]["search_title_rank"]) > number(result[j]["search_title_rank"])
		})
	}
	return result, nil
}

func listingDate(item Record) string {
	if value := str(item, "period_start"); value != "" {
		return value
	}
	if value := str(item, "date"); value != "" {
		return value
	}
	return truncateRunes(str(item, "created"), 10)
}

// caseFold preserves Python's full Unicode fold, including multi-rune mappings.
// The exception table below freezes Python 3.9.6 Unicode 13.0.0 behavior relative
// to Go's Unicode 15.0.0 unicode.ToLower. Generated by enumerating every Unicode
// scalar and comparing Python chr(r).casefold() with Go unicode.ToLower(r).
// It includes Cherokee uppercase folds and overrides newer Go-only mappings.
// Regeneration is a supporting developer probe, never a runtime dependency.
func caseFold(text string) string {
	var out strings.Builder
	for _, r := range text {
		if replacement, ok := pythonFoldOverrides[r]; ok {
			out.WriteString(replacement)
		} else {
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

var pythonFoldOverrides = map[rune]string{
	0x00B5:  "μ",
	0x00DF:  "ss",
	0x0130:  "i̇",
	0x0149:  "ʼn",
	0x017F:  "s",
	0x01F0:  "ǰ",
	0x0345:  "ι",
	0x0390:  "ΐ",
	0x03B0:  "ΰ",
	0x03C2:  "σ",
	0x03D0:  "β",
	0x03D1:  "θ",
	0x03D5:  "φ",
	0x03D6:  "π",
	0x03F0:  "κ",
	0x03F1:  "ρ",
	0x03F5:  "ε",
	0x0587:  "եւ",
	0x13A0:  "Ꭰ",
	0x13A1:  "Ꭱ",
	0x13A2:  "Ꭲ",
	0x13A3:  "Ꭳ",
	0x13A4:  "Ꭴ",
	0x13A5:  "Ꭵ",
	0x13A6:  "Ꭶ",
	0x13A7:  "Ꭷ",
	0x13A8:  "Ꭸ",
	0x13A9:  "Ꭹ",
	0x13AA:  "Ꭺ",
	0x13AB:  "Ꭻ",
	0x13AC:  "Ꭼ",
	0x13AD:  "Ꭽ",
	0x13AE:  "Ꭾ",
	0x13AF:  "Ꭿ",
	0x13B0:  "Ꮀ",
	0x13B1:  "Ꮁ",
	0x13B2:  "Ꮂ",
	0x13B3:  "Ꮃ",
	0x13B4:  "Ꮄ",
	0x13B5:  "Ꮅ",
	0x13B6:  "Ꮆ",
	0x13B7:  "Ꮇ",
	0x13B8:  "Ꮈ",
	0x13B9:  "Ꮉ",
	0x13BA:  "Ꮊ",
	0x13BB:  "Ꮋ",
	0x13BC:  "Ꮌ",
	0x13BD:  "Ꮍ",
	0x13BE:  "Ꮎ",
	0x13BF:  "Ꮏ",
	0x13C0:  "Ꮐ",
	0x13C1:  "Ꮑ",
	0x13C2:  "Ꮒ",
	0x13C3:  "Ꮓ",
	0x13C4:  "Ꮔ",
	0x13C5:  "Ꮕ",
	0x13C6:  "Ꮖ",
	0x13C7:  "Ꮗ",
	0x13C8:  "Ꮘ",
	0x13C9:  "Ꮙ",
	0x13CA:  "Ꮚ",
	0x13CB:  "Ꮛ",
	0x13CC:  "Ꮜ",
	0x13CD:  "Ꮝ",
	0x13CE:  "Ꮞ",
	0x13CF:  "Ꮟ",
	0x13D0:  "Ꮠ",
	0x13D1:  "Ꮡ",
	0x13D2:  "Ꮢ",
	0x13D3:  "Ꮣ",
	0x13D4:  "Ꮤ",
	0x13D5:  "Ꮥ",
	0x13D6:  "Ꮦ",
	0x13D7:  "Ꮧ",
	0x13D8:  "Ꮨ",
	0x13D9:  "Ꮩ",
	0x13DA:  "Ꮪ",
	0x13DB:  "Ꮫ",
	0x13DC:  "Ꮬ",
	0x13DD:  "Ꮭ",
	0x13DE:  "Ꮮ",
	0x13DF:  "Ꮯ",
	0x13E0:  "Ꮰ",
	0x13E1:  "Ꮱ",
	0x13E2:  "Ꮲ",
	0x13E3:  "Ꮳ",
	0x13E4:  "Ꮴ",
	0x13E5:  "Ꮵ",
	0x13E6:  "Ꮶ",
	0x13E7:  "Ꮷ",
	0x13E8:  "Ꮸ",
	0x13E9:  "Ꮹ",
	0x13EA:  "Ꮺ",
	0x13EB:  "Ꮻ",
	0x13EC:  "Ꮼ",
	0x13ED:  "Ꮽ",
	0x13EE:  "Ꮾ",
	0x13EF:  "Ꮿ",
	0x13F0:  "Ᏸ",
	0x13F1:  "Ᏹ",
	0x13F2:  "Ᏺ",
	0x13F3:  "Ᏻ",
	0x13F4:  "Ᏼ",
	0x13F5:  "Ᏽ",
	0x13F8:  "Ᏸ",
	0x13F9:  "Ᏹ",
	0x13FA:  "Ᏺ",
	0x13FB:  "Ᏻ",
	0x13FC:  "Ᏼ",
	0x13FD:  "Ᏽ",
	0x1C80:  "в",
	0x1C81:  "д",
	0x1C82:  "о",
	0x1C83:  "с",
	0x1C84:  "т",
	0x1C85:  "т",
	0x1C86:  "ъ",
	0x1C87:  "ѣ",
	0x1C88:  "ꙋ",
	0x1E96:  "ẖ",
	0x1E97:  "ẗ",
	0x1E98:  "ẘ",
	0x1E99:  "ẙ",
	0x1E9A:  "aʾ",
	0x1E9B:  "ṡ",
	0x1E9E:  "ss",
	0x1F50:  "ὐ",
	0x1F52:  "ὒ",
	0x1F54:  "ὔ",
	0x1F56:  "ὖ",
	0x1F80:  "ἀι",
	0x1F81:  "ἁι",
	0x1F82:  "ἂι",
	0x1F83:  "ἃι",
	0x1F84:  "ἄι",
	0x1F85:  "ἅι",
	0x1F86:  "ἆι",
	0x1F87:  "ἇι",
	0x1F88:  "ἀι",
	0x1F89:  "ἁι",
	0x1F8A:  "ἂι",
	0x1F8B:  "ἃι",
	0x1F8C:  "ἄι",
	0x1F8D:  "ἅι",
	0x1F8E:  "ἆι",
	0x1F8F:  "ἇι",
	0x1F90:  "ἠι",
	0x1F91:  "ἡι",
	0x1F92:  "ἢι",
	0x1F93:  "ἣι",
	0x1F94:  "ἤι",
	0x1F95:  "ἥι",
	0x1F96:  "ἦι",
	0x1F97:  "ἧι",
	0x1F98:  "ἠι",
	0x1F99:  "ἡι",
	0x1F9A:  "ἢι",
	0x1F9B:  "ἣι",
	0x1F9C:  "ἤι",
	0x1F9D:  "ἥι",
	0x1F9E:  "ἦι",
	0x1F9F:  "ἧι",
	0x1FA0:  "ὠι",
	0x1FA1:  "ὡι",
	0x1FA2:  "ὢι",
	0x1FA3:  "ὣι",
	0x1FA4:  "ὤι",
	0x1FA5:  "ὥι",
	0x1FA6:  "ὦι",
	0x1FA7:  "ὧι",
	0x1FA8:  "ὠι",
	0x1FA9:  "ὡι",
	0x1FAA:  "ὢι",
	0x1FAB:  "ὣι",
	0x1FAC:  "ὤι",
	0x1FAD:  "ὥι",
	0x1FAE:  "ὦι",
	0x1FAF:  "ὧι",
	0x1FB2:  "ὰι",
	0x1FB3:  "αι",
	0x1FB4:  "άι",
	0x1FB6:  "ᾶ",
	0x1FB7:  "ᾶι",
	0x1FBC:  "αι",
	0x1FBE:  "ι",
	0x1FC2:  "ὴι",
	0x1FC3:  "ηι",
	0x1FC4:  "ήι",
	0x1FC6:  "ῆ",
	0x1FC7:  "ῆι",
	0x1FCC:  "ηι",
	0x1FD2:  "ῒ",
	0x1FD3:  "ΐ",
	0x1FD6:  "ῖ",
	0x1FD7:  "ῗ",
	0x1FE2:  "ῢ",
	0x1FE3:  "ΰ",
	0x1FE4:  "ῤ",
	0x1FE6:  "ῦ",
	0x1FE7:  "ῧ",
	0x1FF2:  "ὼι",
	0x1FF3:  "ωι",
	0x1FF4:  "ώι",
	0x1FF6:  "ῶ",
	0x1FF7:  "ῶι",
	0x1FFC:  "ωι",
	0x2C2F:  "Ⱟ",
	0xA7C0:  "Ꟁ",
	0xA7D0:  "Ꟑ",
	0xA7D6:  "Ꟗ",
	0xA7D8:  "Ꟙ",
	0xAB70:  "Ꭰ",
	0xAB71:  "Ꭱ",
	0xAB72:  "Ꭲ",
	0xAB73:  "Ꭳ",
	0xAB74:  "Ꭴ",
	0xAB75:  "Ꭵ",
	0xAB76:  "Ꭶ",
	0xAB77:  "Ꭷ",
	0xAB78:  "Ꭸ",
	0xAB79:  "Ꭹ",
	0xAB7A:  "Ꭺ",
	0xAB7B:  "Ꭻ",
	0xAB7C:  "Ꭼ",
	0xAB7D:  "Ꭽ",
	0xAB7E:  "Ꭾ",
	0xAB7F:  "Ꭿ",
	0xAB80:  "Ꮀ",
	0xAB81:  "Ꮁ",
	0xAB82:  "Ꮂ",
	0xAB83:  "Ꮃ",
	0xAB84:  "Ꮄ",
	0xAB85:  "Ꮅ",
	0xAB86:  "Ꮆ",
	0xAB87:  "Ꮇ",
	0xAB88:  "Ꮈ",
	0xAB89:  "Ꮉ",
	0xAB8A:  "Ꮊ",
	0xAB8B:  "Ꮋ",
	0xAB8C:  "Ꮌ",
	0xAB8D:  "Ꮍ",
	0xAB8E:  "Ꮎ",
	0xAB8F:  "Ꮏ",
	0xAB90:  "Ꮐ",
	0xAB91:  "Ꮑ",
	0xAB92:  "Ꮒ",
	0xAB93:  "Ꮓ",
	0xAB94:  "Ꮔ",
	0xAB95:  "Ꮕ",
	0xAB96:  "Ꮖ",
	0xAB97:  "Ꮗ",
	0xAB98:  "Ꮘ",
	0xAB99:  "Ꮙ",
	0xAB9A:  "Ꮚ",
	0xAB9B:  "Ꮛ",
	0xAB9C:  "Ꮜ",
	0xAB9D:  "Ꮝ",
	0xAB9E:  "Ꮞ",
	0xAB9F:  "Ꮟ",
	0xABA0:  "Ꮠ",
	0xABA1:  "Ꮡ",
	0xABA2:  "Ꮢ",
	0xABA3:  "Ꮣ",
	0xABA4:  "Ꮤ",
	0xABA5:  "Ꮥ",
	0xABA6:  "Ꮦ",
	0xABA7:  "Ꮧ",
	0xABA8:  "Ꮨ",
	0xABA9:  "Ꮩ",
	0xABAA:  "Ꮪ",
	0xABAB:  "Ꮫ",
	0xABAC:  "Ꮬ",
	0xABAD:  "Ꮭ",
	0xABAE:  "Ꮮ",
	0xABAF:  "Ꮯ",
	0xABB0:  "Ꮰ",
	0xABB1:  "Ꮱ",
	0xABB2:  "Ꮲ",
	0xABB3:  "Ꮳ",
	0xABB4:  "Ꮴ",
	0xABB5:  "Ꮵ",
	0xABB6:  "Ꮶ",
	0xABB7:  "Ꮷ",
	0xABB8:  "Ꮸ",
	0xABB9:  "Ꮹ",
	0xABBA:  "Ꮺ",
	0xABBB:  "Ꮻ",
	0xABBC:  "Ꮼ",
	0xABBD:  "Ꮽ",
	0xABBE:  "Ꮾ",
	0xABBF:  "Ꮿ",
	0xFB00:  "ff",
	0xFB01:  "fi",
	0xFB02:  "fl",
	0xFB03:  "ffi",
	0xFB04:  "ffl",
	0xFB05:  "st",
	0xFB06:  "st",
	0xFB13:  "մն",
	0xFB14:  "մե",
	0xFB15:  "մի",
	0xFB16:  "վն",
	0xFB17:  "մխ",
	0x10570: "𐕰",
	0x10571: "𐕱",
	0x10572: "𐕲",
	0x10573: "𐕳",
	0x10574: "𐕴",
	0x10575: "𐕵",
	0x10576: "𐕶",
	0x10577: "𐕷",
	0x10578: "𐕸",
	0x10579: "𐕹",
	0x1057A: "𐕺",
	0x1057C: "𐕼",
	0x1057D: "𐕽",
	0x1057E: "𐕾",
	0x1057F: "𐕿",
	0x10580: "𐖀",
	0x10581: "𐖁",
	0x10582: "𐖂",
	0x10583: "𐖃",
	0x10584: "𐖄",
	0x10585: "𐖅",
	0x10586: "𐖆",
	0x10587: "𐖇",
	0x10588: "𐖈",
	0x10589: "𐖉",
	0x1058A: "𐖊",
	0x1058C: "𐖌",
	0x1058D: "𐖍",
	0x1058E: "𐖎",
	0x1058F: "𐖏",
	0x10590: "𐖐",
	0x10591: "𐖑",
	0x10592: "𐖒",
	0x10594: "𐖔",
	0x10595: "𐖕",
}
