# Контракт map-reader

Единый бинарный контракт по всем моделям, публикуемый в POSIX shared memory
(`docs/map-reader.md`). **См. также:** `docs/map-memory.md` (адреса), `docs/stat.md`
(контракты телеметрии для хранилища). Значения **сырые** (масштабирование — на потребителе),
многобайтовые ячейки слиты (little-endian), битовые карты раскладываются
потребителем по описанию.

Формат: `version = 5`, полей — `402`. Версия 5: **добавлены ячейки в конец списка**
(id 392..401): `E_T_CHARGE_NEED_ECO` (0x07F), `TdizProfilact_day` (0x0DB),
`LCD_Rele3_Funk/L1/L2/Gist` (0x120..0x123), `LCD_ReleII_Funk/L1/L2/Gist`
(0x124..0x127); добавлен регион чтения `0x120..0x127`. Версия 4: добавлен блок BMS
(банки `UakbCell_L/H0..31` 0x480..0x4BF, `TakbCell0..31` 0x4C0..0x4DF,
`Q_Cell0..31` 0x4E0..0x4FF) и регион чтения `0x480..0x4FF`. Версия 3: очистка
контракта от бит-значений/шума и добавление `LCD_MPPTNum`.
Источник — `protocol_MAP_cells_2026_07_15`.

## Зависимости контракта (менять синхронно!)

`id` = **позиция в `documentedCells`** (`daemons/map-reader/cells.go`). Любое
добавление/удаление/перестановка ячеек затрагивает ВСЕ места ниже; map-reader и
web выкладывать **одним релизом** (иначе payload разойдётся с декодером).

- `daemons/map-reader`: `cells.go` (`documentedCells`, `mapRegions`),
  `contract.go` (`buildContract`, `DecodePayload`,
  `readRegionsNative/Modbus`, `contractVersion`), `mapdata.go` (структура `MapData`
  — порядок полей = id), `poll.go` (`buildContract`, `mapRegions`),
  `write.go` (`buildContract`).
- `web` (Go): `internal/web/mapfields.go` (`mapFieldNames`), `internal/web/legacy_cells.go`
  (`mapCellAddr`, `mapCellSize`), `internal/web/legacy.go` (`legacyFieldIdx`, `buildMem`),
  `internal/web/legacyapi.go` (размеры по `mapFieldNames`), `internal/web/maps.go`
  (`data["FieldNames"]`).
- **Версия контракта пиннится у потребителя:** `internal/web/legacy.go`
  (`legacyContractVersion`) обязан совпадать с `contractVersion` в
  `daemons/map-reader/contract.go`. При расхождении `decodeLegacy` отвергает
  payload, снимок `read_json.php` не обновляется, `check_daemons.php` отдаёт
  `mapd=1`. Потребитель этой связки — внешний легаси-агрегатор **«Мастер-Узел»**
  (`master_node.php` на других узлах, через `request_node.php`), который при
  `mapd=1` не рисует блок МАП и телеметрия «не идёт» — см.
  `docs/legacy/master-node.md`. Панель `assets/static/map.js` версию не сверяет
  (только магию и `nfields`), поэтому локальный UI может работать, а легаси-узел
  нет. Равенство версий проверяется тестом
  `web/internal/web/legacy_contract_test.go` (падает при расхождении).
- Фронтенд: `assets/templates/map.html` (`window.MAP_FIELDS`), `assets/static/map.js`
  (декод payload по индексу), `assets/static/mapinfo.js`, `assets/static/control.js`
  (`window.MAP_CONTROL.addr2fid`), панель 2 (`assets/static/legacy/panel2.js` — тот же
  `MAP_FIELDS`), панель 3 (`assets/static/legacy_map.js` — через легаси-API).
- Легаси-API: `read_json.php`/`read_memory.php`/`check_daemons.php` (через `legacy.go`)
  — `read_memory` собирается из `mapCellAddr/Size`.
- Генератор: `web/tools/gen_control.py` (читает `cells.go` → `addr2fid`).
- Док: `docs/map-memory.md` (регионы).

Пример: добавление 4 ячеек Rele3 требовало правки `cells.go` (+регионы),
`mapdata.go`, `mapfields.go`, `legacy_cells.go`, реген `control-data` и версии
контракта.

## Сегмент shm и заголовки (полный формат)

Именованный сегмент POSIX (`/dev/shm/malina-map-state`), один писатель-процесс:
```
[header 64B][list: slots×72B][slots: slots×slotSize]
```
`slots = 9`, `slotSize = 32 + 16384 = 16416`, общий размер = 64 + 9×72 + 9×16416.

**Заголовок сегмента (64 Б):**
```
0  magic    u32   = 0x314D4150 ("PAM1")
4  version  u32   (текущая 3)
8  slotSize u32   (16416)
12 slots    u32   (9)
16..63      резерв (нули)
```

**Запись списка `ListItem` (72 Б), по одной на слот:**
```
0  state        u32  (0 свободен / 1 активен)
4  model        u8   ('P'/'H'/'D'/'T')
5  protocol     u8   (0=Микроарт, 1=Modbus)
6  unit         u16
8  seq          u64  (seqlock: нечёт=идёт запись)
16 gen          u64  (номер «аренды» слота = timestamp назначения, `uint64`)
24 baud         u32
28 serial       u16
30 serialLetter u8
31 reserved     u8
32 port         [32]byte
64 lastSeen     u64  (unix)
```

**Заголовок слота (32 Б), далее payload:**
```
0  state    u32  (0 свободен / 1 активен; =1 только после первой публикации)
4  reserved u32
8  gen      u64
16 seq      u64  (seqlock)
24 len      u32  (длина payload)
28 crc      u32  (CRC32 IEEE payload)
32 ..      payload (см. ниже)
```

Доступ по `offset = header + i*listItemSize` (список) и
`header + slots*listItemSize + i*slotSize` (слоты). Seqlock: писатель делает `seq`
нечётным на время записи и чётным по завершении; читатель берёт `seq` до и после и
сверяет (плюс `crc32`/`len`payload). Реализация — `shm.go`.

## Полезная нагрузка слота (payload)

```
0   magic uint32 ('M','A','P','D')
4   version uint16
6   nfields uint16
8   port [32]byte
40  model uint8
41  protocol uint8 (0=Микроарт,1=Modbus)
42  unit uint8
43  reserved uint8
44  reserved uint32
48  gen uint64
56  lastSeen uint64
64  presence [ceil(nfields/8)]byte
64+presenceLen  values nfields×uint32 LE
```

Декодер — `contract.go:DecodePayload` → `ContractMeta` + `MapData` + `presence`.

## Go-структура контракта (`mapdata.go`)

```go
type MapData struct {
	Device uint32 // id=0 0x000 1B
	VerPow uint32 // id=1 0x001 1B
	VerPO uint32 // id=2 0x002 1B
	FUnet1_Norm uint32 // id=3 0x002 1B
	ComMAP_EEPromWR uint32 // id=4 0x003 1B
	ErrD_I2C_BMS uint32 // id=5 0x004 1B
	FUnet2_Norm uint32 // id=6 0x004 1B
	POW uint32 // id=7 0x005 1B
	UACC uint32 // id=8 0x006 1B
	DevOpt uint32 // id=9 0x007 1B
	A uint32 // id=10 0x008 1B
	ErrD_I2C_MPPT uint32 // id=11 0x008 1B
	RAM_END_L uint32 // id=12 0x008 1B
	RAM_END_H uint32 // id=13 0x009 1B
	Language uint32 // id=14 0x00A 1B
	N uint32 // id=15 0x00A 1B
	XF6 uint32 // id=16 0x00A 1B
	VerPlatPic uint32 // id=17 0x00C 1B
	VerPlatNet uint32 // id=18 0x00D 1B
	VerPlatPowDop uint32 // id=19 0x00E 1B
	VerTest uint32 // id=20 0x00F 1B
	ErrD_I2C_verPO4_MPPT uint32 // id=21 0x010 1B
	XF0 uint32 // id=22 0x010 1B
	TFT_CIChargeSOC95 uint32 // id=23 0x013 1B
	FUAcc_Korr uint32 // id=24 0x016 1B
	SerialNum0 uint32 // id=25 0x018 1B
	SerialNum1 uint32 // id=26 0x019 1B
	T_ButCh_r uint32 // id=27 0x01A 1B
	T_Diz_prof uint32 // id=28 0x01B 1B
	POW_Korr uint32 // id=29 0x01C 1B
	I_Chage_Korr uint32 // id=30 0x01D 1B
	TempTopToBMS uint32 // id=31 0x021 1B
	SerialNum2 uint32 // id=32 0x022 1B
	SerialNum3 uint32 // id=33 0x023 1B
	SyncDiz_Plat uint32 // id=34 0x024 1B
	No3FazeMotor uint32 // id=35 0x025 1B
	T_AccDisch_ExtH uint32 // id=36 0x026 1B
	TFT_SOC_DisCharge uint32 // id=37 0x02C 1B
	TFT_SOC_StartCharge uint32 // id=38 0x02D 1B
	TFT_SOC_DizStart uint32 // id=39 0x02E 1B
	PNET_Prodag_MAX uint32 // id=40 0x033 1B
	DigitInFunc1 uint32 // id=41 0x034 1B
	DigitInFunc2 uint32 // id=42 0x035 1B
	AVR_On uint32 // id=43 0x03E 1B
	Grid_52Hz uint32 // id=44 0x03F 1B
	ErrS_TorPolarity uint32 // id=45 0x040 1B
	CulerErr uint32 // id=46 0x048 1B
	MODE uint32 // id=47 0x050 1B
	MASK_DEV_ON uint32 // id=48 0x051 1B
	NUMCULLER uint32 // id=49 0x052 1B
	WATCH uint32 // id=50 0x053 2B
	WATCH2 uint32 // id=51 0x055 1B
	UAccAfterDisChage uint32 // id=52 0x05B 1B
	FrozenUAccAfterDisChage uint32 // id=53 0x05D 1B
	NetUpECO_net2_off uint32 // id=54 0x05F 1B
	DetTforDopRele uint32 // id=55 0x060 1B
	LCD_UAccDizStart_dop uint32 // id=56 0x061 1B
	RSBAUD_Rasb_Load uint32 // id=57 0x064 1B
	CIChargeAbsorb uint32 // id=58 0x065 1B
	T_MaxLiIonBMS uint32 // id=59 0x066 1B
	Del_UAccChBUF_24h uint32 // id=60 0x067 1B
	PowAccNom uint32 // id=61 0x068 1B
	X94 uint32 // id=62 0x06C 1B
	NUM_Overload uint32 // id=63 0x073 1B
	T_CHARGE uint32 // id=64 0x078 1B
	T_CHARGE_UmaxECO_diz uint32 // id=65 0x079 1B
	DelUChargeEnd uint32 // id=66 0x07A 1B
	T_NET_ON uint32 // id=67 0x07B 1B
	T_CHARGE_Umax uint32 // id=68 0x07E 1B
	X81 uint32 // id=69 0x07F 1B
	T_ACCDISCH uint32 // id=70 0x080 1B
	T_NOMINAL uint32 // id=71 0x083 1B
	KorrTempAkb uint32 // id=72 0x085 1B
	LCD_UNET2Up uint32 // id=73 0x089 1B
	LCD_UNET2Down uint32 // id=74 0x08A 1B
	X6F uint32 // id=75 0x091 1B
	X36 uint32 // id=76 0x0CA 1B
	X33 uint32 // id=77 0x0CD 1B
	X2E uint32 // id=78 0x0D2 1B
	ReleNetNum uint32 // id=79 0x0D7 1B
	ReleIzbNum uint32 // id=80 0x0D8 1B
	TCh_MPPT_GRID52 uint32 // id=81 0x0DA 1B
	DataRS uint32 // id=82 0x0DB 1B
	TchEco_14day uint32 // id=83 0x0DC 1B
	MinTarifAnCh uint32 // id=84 0x0DE 1B
	MinTarifCh uint32 // id=85 0x0DF 1B
	CAP_OnOff uint32 // id=86 0x0F6 1B
	P_ModemVdd uint32 // id=87 0x0FE 1B
	Sum uint32 // id=88 0x0FF 1B
	BMS_Num uint32 // id=89 0x100 1B
	S uint32 // id=90 0x100 1B
	LCD_CAcc_dop uint32 // id=91 0x101 1B
	LCD_UAccMin_dop uint32 // id=92 0x102 1B
	LCD_UAccMinNetGen_dop uint32 // id=93 0x103 1B
	LCD_UAccChMax_dop uint32 // id=94 0x104 1B
	LCD_UAccChBUF_dop uint32 // id=95 0x105 1B
	LCD_UAccChStart_dop uint32 // id=96 0x106 1B
	LCD_NetMaxPow_dop uint32 // id=97 0x107 1B
	LCD_TypeSin uint32 // id=98 0x138 1B
	LCD_MAP_iFaze uint32 // id=99 0x139 1B
	LCD_UMAP220_NEED uint32 // id=100 0x13A 1B
	LCD_NetUpLoad uint32 // id=101 0x13B 1B
	LCD_NetUpECO uint32 // id=102 0x13C 1B
	LCD_UAccMin uint32 // id=103 0x13D 1B
	LCD_SensLoad uint32 // id=104 0x13E 1B
	LCD_MAP_Sync uint32 // id=105 0x13F 1B
	LCD_NetDizel uint32 // id=106 0x150 1B
	LCD_Net2 uint32 // id=107 0x151 1B
	LCD_DizelMaxPow uint32 // id=108 0x152 1B
	LCD_UAccDizStart uint32 // id=109 0x153 1B
	LCD_SlaveMAPNum uint32 // id=110 0x155 1B
	LCD_BMSFunk uint32 // id=111 0x156 1B
	LCD_NetMaxPow uint32 // id=112 0x168 1B
	LCD_UNETUp uint32 // id=113 0x169 1B
	LCD_UNETDown uint32 // id=114 0x16A 1B
	LCD_NETAlg uint32 // id=115 0x16B 1B
	LCD_UAccMinNetGen uint32 // id=116 0x16C 1B
	LCD_TimeEcoStart uint32 // id=117 0x16D 1B
	LCD_TimeEcoEnd uint32 // id=118 0x16E 1B
	LCD_PercentECOMinGen uint32 // id=119 0x16F 1B
	LCD_AccType uint32 // id=120 0x180 1B
	LCD_CAcc uint32 // id=121 0x181 1B
	LCD_CIChargeStart uint32 // id=122 0x182 1B
	LCD_CIChargeEnd uint32 // id=123 0x183 1B
	LCD_ChargeAlg uint32 // id=124 0x184 1B
	LCD_UAccChMax uint32 // id=125 0x185 1B
	LCD_UAccChBUF uint32 // id=126 0x186 1B
	LCD_UAccChStart uint32 // id=127 0x187 1B
	LCD_Rele1_Funk uint32 // id=128 0x198 1B
	LCD_Rele1_U_T_min uint32 // id=129 0x199 1B
	LCD_Rele1_U_T_maxInv uint32 // id=130 0x19A 1B
	LCD_Rele1_GistInv uint32 // id=131 0x19B 1B
	LCD_Rele2_Funk uint32 // id=132 0x19C 1B
	LCD_Rele2_U_T_min uint32 // id=133 0x19D 1B
	LCD_Rele2_U_T_maxInv uint32 // id=134 0x19E 1B
	LCD_Rele2_GistInv uint32 // id=135 0x19F 1B
	LCD_Vers uint32 // id=136 0x1B0 1B
	LCD_LCDType uint32 // id=137 0x1B2 1B
	LCD_Sound uint32 // id=138 0x1B3 1B
	LCD_SignalNetOff uint32 // id=139 0x1B4 1B
	LCD_RSBAUD uint32 // id=140 0x1B5 1B
	E_LCD_TimeCyr uint32 // id=141 0x1B6 1B
	LCD_TimeCyr uint32 // id=142 0x1B6 1B
	LCD_T_MAXCharge uint32 // id=143 0x1B7 1B
	LCD_RasberyON uint32 // id=144 0x1C9 1B
	LCD_DevAdr uint32 // id=145 0x1CA 1B
	LCD_RS_Protocol uint32 // id=146 0x1CB 1B
	LCD_AddDevice uint32 // id=147 0x1CC 1B
	LCD_RSBAUD485 uint32 // id=148 0x1CD 1B
	LCD_RS485_Num uint32 // id=149 0x1CE 1B
	LCDmax_T_MAXCharge uint32 // id=150 0x1CF 1B
	StatusCh uint32 // id=151 0x402 1B
	Put_eeprom uint32 // id=152 0x403 1B
	StateUAcc uint32 // id=153 0x404 1B
	UAcc_med_VH uint32 // id=154 0x405 1B
	UAcc_med_VL uint32 // id=155 0x406 1B
	IAcc_med_A_2 uint32 // id=156 0x408 1B
	PLoad_L uint32 // id=157 0x409 1B
	T_NetOn uint32 // id=158 0x40B 1B
	T_Charge uint32 // id=159 0x40C 1B
	T_AccDisch_L uint32 // id=160 0x40E 1B
	T_Nominal uint32 // id=161 0x40F 1B
	T_Overload uint32 // id=162 0x410 1B
	TOff_Overload uint32 // id=163 0x411 1B
	F_AccOver uint32 // id=164 0x41C 1B
	F_NETOver uint32 // id=165 0x41D 1B
	UNET uint32 // id=166 0x422 1B
	INET uint32 // id=167 0x423 1B
	PNET_L uint32 // id=168 0x424 1B
	TFNET uint32 // id=169 0x425 1B
	ThFMAP uint32 // id=170 0x426 1B
	UOUTmed uint32 // id=171 0x427 1B
	TFNET_Limit uint32 // id=172 0x428 1B
	UNET_Limit uint32 // id=173 0x429 1B
	RSErrSis uint32 // id=174 0x42A 1B
	RSErrJobM uint32 // id=175 0x42B 1B
	RSErrJob uint32 // id=176 0x42C 1B
	RSWarning uint32 // id=177 0x42D 1B
	Temp_Grad0 uint32 // id=178 0x42E 1B
	Temp_Grad1 uint32 // id=179 0x42F 1B
	Temp_Grad2 uint32 // id=180 0x430 1B
	INET_16_4 uint32 // id=181 0x431 1B
	IAcc_med_A_u16_L uint32 // id=182 0x432 1B
	IAcc_med_A_u16_H uint32 // id=183 0x433 1B
	RCON_img uint32 // id=184 0x439 1B
	RCON_img_L uint32 // id=185 0x439 1B
	Temp_off uint32 // id=186 0x43C 1B
	E_LCD_UAccChMax uint32 // id=187 0x43F 1B
	E_LCD_UAccChBUF uint32 // id=188 0x440 1B
	T_CHARGE_NEED_ECO uint32 // id=189 0x441 1B
	RCON_img_H uint32 // id=190 0x446 1B
	RSErrDop uint32 // id=191 0x447 1B
	ThFMAPSync uint32 // id=192 0x449 1B
	TimeCyr_MINUT uint32 // id=193 0x44B 1B
	BMS uint32 // id=194 0x44C 1B
	M_POWhourNET_L uint32 // id=195 0x44D 1B
	M_POWhourNET_x uint32 // id=196 0x44D 3B
	M_POWhourNET_H uint32 // id=197 0x44E 1B
	M_POWhourNET_HH uint32 // id=198 0x44F 1B
	M_POWhourMAP_L uint32 // id=199 0x450 1B
	M_POWhourMAP_H uint32 // id=200 0x451 1B
	M_POWhourMAP_HH uint32 // id=201 0x452 1B
	M_POWhourMAPCharge_L uint32 // id=202 0x453 1B
	M_POWhourMAPCharge_H uint32 // id=203 0x454 1B
	M_POWhourMAPCharge_HH uint32 // id=204 0x455 1B
	PLoad_H uint32 // id=205 0x456 1B
	PNET_H uint32 // id=206 0x457 1B
	CulerSpead uint32 // id=207 0x458 1B
	Fl_UAccChBUF_24h uint32 // id=208 0x516 1B
	UAcc_Optim_V uint32 // id=209 0x520 1B
	UAcc_Optim_VL uint32 // id=210 0x520 1B
	UAcc_Optim_VH uint32 // id=211 0x521 1B
	UAcc_med_VS_100L uint32 // id=212 0x522 1B
	UAcc_med_VS_100H uint32 // id=213 0x523 1B
	MPPT_toCh uint32 // id=214 0x524 1B
	FlagUnet2 uint32 // id=215 0x527 1B
	I_Akb_MAP_faz1L uint32 // id=216 0x528 1B
	I_Akb_MAP_faz1H uint32 // id=217 0x529 1B
	I_Akb_MAP_faz2L uint32 // id=218 0x52A 1B
	I_Akb_MAP_faz2H uint32 // id=219 0x52B 1B
	I_Akb_MAP_faz3L uint32 // id=220 0x52C 1B
	I_Akb_MAP_faz3H uint32 // id=221 0x52D 1B
	T_AccDisch uint32 // id=222 0x52E 1B
	TdizProfilact_day_cnt uint32 // id=223 0x582 1B
	TchEco_Ch uint32 // id=224 0x583 1B
	Flag_NETON_ECO uint32 // id=225 0x585 1B
	Status_RELEdop uint32 // id=226 0x586 1B
	PNET_Sign_P uint32 // id=227 0x587 1B
	LCD_UAccChMax_S_Temp uint32 // id=228 0x588 2B
	Pmax_On uint32 // id=229 0x58C 1B
	PmaxGenDisch uint32 // id=230 0x58D 1B
	M_POWhourNET_sign uint32 // id=231 0x592 1B
	PNET_8 uint32 // id=232 0x59A 2B
	PLoad_8 uint32 // id=233 0x59E 2B
	UAccMinNetGen_S uint32 // id=234 0x5DC 2B
	SoC uint32 // id=235 0x718 2B
	SoH uint32 // id=236 0x71A 2B
}
```

## Список (`MapList`) — бинарная коллекция

Сегмент: `[header 64B][list: slots×72B][slots: slots×slotSize]`.

`ListItem` (72 Б): state u32, model u8, protocol u8, unit u16, seq u64,
gen u64, baud u32, serial u16, serialLetter u8, port[32], lastSeen u64.

Go-классы — `maplist.go`: `MapListItem`, `MapList`, `DecodeList(data)`.
Читатель: `DecodeList` → элемент `(ID, Gen)` → слот `ID` → `DecodePayload`.


## Поля (id, адрес, размер)

Канонический список всех полей контракта (`id` = позиция в массиве `documentedCells`) —
в коде: **`daemons/map-reader/cells.go`**. Там же адрес и размер каждой ячейки;
регионы чтения — единый `mapRegions` для всех моделей (там же). Адреса и регионы по
памяти МАП — `docs/map-memory.md`; детальные описания и признак модели
(«Титанатор») — в исходном документе `protocol_MAP_cells_2026_07_15`.

> Ранее таблица полей была продублирована здесь вручную и **расходилась** с
> `cells.go` (устаревшие бит-строки сбивали нумерацию `id`) — удалена, чтобы
> не вводить в заблуждение. Источник истины — `cells.go`.

- `id` = позиция в `documentedCells` (`daemons/map-reader/cells.go`).
- `Далее` — адрес/размер/регионы телеметрии: `docs/stat.md` (контракты для tsdb,
  осознанное подмножество полей со **своей** нумерацией), `docs/map-memory.md` (карта).
