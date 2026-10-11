// Package device — общие для демонов ПАК «Малина» сведения об изделиях Микроарт:
// коды устройства (ячейка _Device, 0x0000) и минимальный разбор сигнатуры.
//
// Коды подтверждены дизасмом legacy-утилиты usbscanner (см.
// docs/legacy/kes-legacy-malina2.md §1a): D3=МАП, D5=КЭС/MPPT, D7/D9=зарядник.
package device

// Коды устройства (_Device, ячейка 0x0000).
const (
	TypeUnknown  byte = 0 // не распознано
	TypeMap      byte = 3 // МАП (MAC)
	TypeKes      byte = 5 // КЭС / MPPT
	TypeCharger  byte = 7 // зарядник (вариант 1)
	TypeCharger2 byte = 9 // зарядник (вариант 2)
)

// Name возвращает человекочитаемое имя кода устройства.
func Name(code byte) string {
	switch code {
	case TypeMap:
		return "МАП"
	case TypeKes:
		return "КЭС"
	case TypeCharger, TypeCharger2:
		return "зарядник"
	default:
		return "неизвестно"
	}
}
