package web

import "strconv"

// modeLabelStatic — статический текст режима по номеру (для настройки/списка).
// Для 9 («несколько бит StatusCh») — «…Прочие режимы».
func (s *Server) modeLabelStatic(lang string, code uint32) string {
	if code == 9 {
		return s.tr(lang, "events.mode9")
	}
	return s.tr(lang, "p1.mode"+strconv.FormatUint(uint64(code), 10))
}

// modeTextDynamic — текущий текст режима; для 9 добавляем число и двоичное StatusCh.
func (s *Server) modeTextDynamic(lang string, code, statusCh uint32) string {
	if code == 9 {
		return s.tr(lang, "p1.mode9_prefix") + " " +
			strconv.FormatUint(uint64(statusCh), 10) + " (" +
			strconv.FormatUint(uint64(statusCh), 2) + ")"
	}
	return s.tr(lang, "p1.mode"+strconv.FormatUint(uint64(code), 10))
}
