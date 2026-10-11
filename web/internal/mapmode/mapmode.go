// Package mapmode — единая серверная функция вычисления режима МАП.
// Один источник правды: используется и legacy-API (internal/web/legacy.go),
// и менеджером событий (internal/events). Клиент (панель 1) считает сам из
// бинарного сырья — осознанное дублирование.
//
// Алгоритм — плоский вертикальный список «первое совпадение», только по кодам
// полей МАП, без промежуточных переменных. См. docs/legacy_read_json_map.md.
package mapmode

// Mode — режим по кодам полей МАП. reg(field) возвращает значение ячейки.
//
// Режим заряда (_MODE > 3) уточняется ступенью из _StatusCh (точное равенство):
// 0x00→4, 0x01→5, 0x02→6, 0x04→7, 0x08→8, иначе 9.
func Mode(reg func(field string) uint32) uint32 {
	switch {
	case reg("MODE_RAM") <= 1:
		return reg("MODE_RAM") // 0: МАП выключен и нет сети; 1: выключен, сеть на входе
	case reg("MODE_RAM") > 3 && reg("StatusCh") == 0x00:
		return 4 // МАП включен. Трансляция + заряд. Трансляция
	case reg("MODE_RAM") > 3 && reg("StatusCh") == 0x01:
		return 5 // МАП включен. Трансляция + заряд. Буферный заряд
	case reg("MODE_RAM") > 3 && reg("StatusCh") == 0x02:
		return 6 // МАП включен. Трансляция + заряд. Заряд 1 ступень
	case reg("MODE_RAM") > 3 && reg("StatusCh") == 0x04:
		return 7 // МАП включен. Трансляция + заряд. Заряд 2 ступень
	case reg("MODE_RAM") > 3 && reg("StatusCh") == 0x08:
		return 8 // МАП включен. Трансляция + заряд. Дозаряд 3 ступень
	case reg("MODE_RAM") > 3:
		// Панель: "МАП включен. Трансляция + заряд. Режим <_StatusCh dec> (<_StatusCh bin>)"
		// Dropdown (события): "МАП включен. Трансляция + заряд. Прочие режимы"
		return 9
	case reg("LCD_NetUpECO") == 0 && reg("LCD_NetUpLoad") == 1 && reg("Pmax_On")&2 != 0 && reg("UNET") > 100:
		return 18 // Подкачка Pmax
	case reg("LCD_NetUpECO") == 0 && reg("MODE_RAM") == 2 && reg("UNET") > 100 && reg("LCD_NETAlg") == 2:
		return 10 // Принудительная генерация
	case reg("LCD_NetUpECO") == 0 && reg("MODE_RAM") == 2 && reg("UNET") > 100 && reg("LCD_NETAlg") == 3 && reg("flag_NETON_ECO")&2 == 0:
		return 11 // Тарифная сеть. максимальный тариф. принудительная генерация
	case reg("LCD_NetUpECO") == 0 && reg("MODE_RAM") == 2 && reg("UNET") > 100 && reg("LCD_NETAlg") == 3:
		return 12 // Тарифная сеть. минимальный тариф
	case reg("LCD_NetUpECO") == 0 && reg("MODE_RAM") == 2:
		return 2 // МАП включен. Генерация от АКБ. Нет сети.
	case reg("LCD_NetUpECO") == 0:
		return 3 // МАП включен и транслирует сеть
	case reg("MODE_RAM") == 3 && reg("LCD_NetUpECO") == 1 && reg("LCD_NETAlg") == 2 && reg("flag_NETON_ECO")&1 != 0:
		return 15 // Ожидание внешнего полного заряда
	case reg("MODE_RAM") == 3 && reg("LCD_NetUpECO") == 1 && reg("LCD_NETAlg") == 2:
		return 13 // Трансляция + эко-подкачка
	case reg("MODE_RAM") == 3 && reg("LCD_NetUpECO") == 1 && reg("LCD_NETAlg") == 3 && reg("flag_NETON_ECO")&2 != 0:
		return 12 // Тарифная сеть. минимальный тариф
	case reg("MODE_RAM") == 3 && reg("LCD_NetUpECO") == 1 && reg("LCD_NETAlg") == 3:
		return 16 // Тарифная сеть. трансляция+эко-подкачка
	case reg("MODE_RAM") == 3 && reg("LCD_NetUpECO") == 2 && reg("LCD_NETAlg") == 2 && reg("flag_NETON_ECO")&1 != 0:
		return 15 // Ожидание внешнего полного заряда
	case reg("MODE_RAM") == 3 && reg("LCD_NetUpECO") == 2 && reg("LCD_NETAlg") == 2:
		return 14 // Трансляция + продажа в сеть
	case reg("MODE_RAM") == 3 && reg("LCD_NetUpECO") == 2 && reg("LCD_NETAlg") == 3 && reg("flag_NETON_ECO")&2 != 0:
		return 12 // Тарифная сеть. минимальный тариф
	case reg("MODE_RAM") == 3 && reg("LCD_NetUpECO") == 2 && reg("LCD_NETAlg") == 3:
		return 17 // Тарифная сеть. трансляция+продажа в сеть
	case reg("MODE_RAM") == 2:
		return 2 // МАП включен. Генерация от АКБ. Нет сети.
	}
	return 3 // МАП включен и транслирует сеть
}
