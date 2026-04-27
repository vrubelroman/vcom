package ui

import tea "github.com/charmbracelet/bubbletea"

// cyrillicToLatin maps Russian ЙЦУКЕН characters to their QWERTY positional
// equivalents. This allows all single-letter commands to work regardless of
// whether the user has a Russian or Latin keyboard layout active.
var cyrillicToLatin = map[rune]rune{
	// Lowercase — same physical key position
	'й': 'q', 'ц': 'w', 'у': 'e', 'к': 'r', 'е': 't', 'н': 'y',
	'г': 'u', 'ш': 'i', 'щ': 'o', 'з': 'p', 'х': '[', 'ъ': ']',
	'ф': 'a', 'ы': 's', 'в': 'd', 'а': 'f', 'п': 'g', 'р': 'h',
	'о': 'j', 'л': 'k', 'д': 'l', 'ж': ';', 'э': '\'',
	'я': 'z', 'ч': 'x', 'с': 'c', 'м': 'v', 'и': 'b', 'т': 'n',
	'ь': 'm', 'б': ',', 'ю': '.', 'ё': '`',

	// Uppercase — same physical key position with Shift
	'Й': 'Q', 'Ц': 'W', 'У': 'E', 'К': 'R', 'Е': 'T', 'Н': 'Y',
	'Г': 'U', 'Ш': 'I', 'Щ': 'O', 'З': 'P', 'Х': '{', 'Ъ': '}',
	'Ф': 'A', 'Ы': 'S', 'В': 'D', 'А': 'F', 'П': 'G', 'Р': 'H',
	'О': 'J', 'Л': 'K', 'Д': 'L', 'Ж': ':', 'Э': '"',
	'Я': 'Z', 'Ч': 'X', 'С': 'C', 'М': 'V', 'И': 'B', 'Т': 'N',
	'Ь': 'M', 'Б': '<', 'Ю': '>', 'Ё': '~',
}

// translateKeyMsg translates Cyrillic characters in a KeyMsg to their Latin
// positional equivalents (ЙЦУКЕН → QWERTY). If no translation is needed, the
// original message is returned unchanged.
func translateKeyMsg(msg tea.KeyMsg) tea.KeyMsg {
	if msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
		return msg
	}
	translated := make([]rune, len(msg.Runes))
	changed := false
	for i, r := range msg.Runes {
		if latin, ok := cyrillicToLatin[r]; ok {
			translated[i] = latin
			changed = true
		} else {
			translated[i] = r
		}
	}
	if !changed {
		return msg
	}
	return tea.KeyMsg{
		Type:  msg.Type,
		Runes: translated,
		Alt:   msg.Alt,
		Paste: msg.Paste,
	}
}
