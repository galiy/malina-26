# Контракт map-reader

Единый бинарный контракт по всем моделям, публикуемый в POSIX shared memory
(`docs/map-reader.md`). Значения **сырые** (масштабирование — на потребителе),
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

- `daemons/map-reader`: `cells.go` (`documentedCells`, `regionsT`, `regionsPHD`,
  `regionsFor`), `contract.go` (`buildContract`, `DecodePayload`,
  `readRegionsNative/Modbus`, `contractVersion`), `mapdata.go` (структура `MapData`
  — порядок полей = id), `poll.go`/`write.go` (`buildContract`, `regionsFor`).
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

## Поля (id, адрес, размер, модель, описание)

«Модель»: `T` — только Титанатор; пусто — общая / Pro/Hybrid/Dominator.

| id | Тег | Поле Go | Адрес | Б | М | Описание |
|---|---|---|---|---|---|---|
| 0 | `Device` | `Device` | 0x000 | 1 |  | _Device=0x00 код устройства, =3 для МАП (другие коды соответствуют другим изделиям фирмы Микроарт). |
| 1 | `VerPow` | `VerPow` | 0x001 | 1 |  | _VerPow=0x01 - версия текущей модели Pow. Начиная с версии ПО 16, старший бит (8-й) 1 – означает гибрид, 7-й и 6 бит зарезервирован. Младшие 5 бит версия силовой платы. |
| 2 | `VerPO` | `VerPO` | 0x002 | 1 |  | _VerPO=0x02 - версия ПO для данного устройства, старшие 3бита - значение после точки: |
| 3 | `fUnet1_Norm` | `FUnet1_Norm` | 0x002 | 1 |  | Бит1: fUnet1_Norm=0x02 - есть сеть на первом входе. |
| 4 | `ComMAP_EEPromWR` | `ComMAP_EEPromWR` | 0x003 | 1 |  | w 0x00 0x00 0x00 DS\n, где в данном случае, D=ComMAP_EEPromWR=0x03, т.е. |
| 5 | `ErrD_I2C_BMS` | `ErrD_I2C_BMS` | 0x004 | 1 |  | Бит2: ErrD_I2C_BMS=0x04 - ошибка I2C для BMS. |
| 6 | `fUnet2_Norm` | `FUnet2_Norm` | 0x004 | 1 |  | Бит2: fUnet2_Norm=0x04 - есть сеть на втором входе. |
| 7 | `POW` | `POW` | 0x005 | 1 |  | _POW=0x05 - Мощность устройства. Значения от 0 до11 последовательно соответствуют мощностям: |
| 8 | `UACC` | `UACC` | 0x006 | 1 |  | _UACC=0x06 - Рабочее напряжение МАП. Значения от 0 до 3 последовательно соответствуют напряжениям: |
| 9 | `DevOpt` | `DevOpt` | 0x007 | 1 |  | _DevOpt=0x07 - Версия процессора: 1 - для для МАП Sin, Доминатор; 2-снята с производства; 3 – Для модели Титанатор. |
| 10 | `A` | `A` | 0x008 | 1 |  | чтение байта (страницы длиной 1 байт, т.е. P=0) с адреса A=0x0084. |
| 11 | `ErrD_I2C_MPPT` | `ErrD_I2C_MPPT` | 0x008 | 1 |  | Бит3: ErrD_I2C_MPPT=0x08 - ошибка I2C для MPPT. |
| 12 | `RAM_END_L` | `RAM_END_L` | 0x008 | 1 |  | _RAM_END_L=0x08, _RAM_END_H=0x09 – адрес конечной ячейки RAM |
| 13 | `RAM_END_H` | `RAM_END_H` | 0x009 | 1 |  | _RAM_END_L=0x08, _RAM_END_H=0x09 – адрес конечной ячейки RAM |
| 14 | `Language` | `Language` | 0x00A | 1 |  | _Language=0x0A – 0 – Русский язык меню, 1-английский. Есть также во всех моделях но только на чтение, а в Титанаторе язык можно изменить программированием этой ячейки. Пункт мен… |
| 15 | `n` | `N` | 0x00A | 1 |  | Протокол передает данные, конец посылки является символ \n=0x0A (LF – новая строка). Если надо в посылке передать символ \n, то вместо него вставляется последовательность 0xDB, … |
| 16 | `xF6` | `XF6` | 0x00A | 1 |  | где S=0x100-mod256(0x72+0x00+0x00+0x84)= 0x100-mod256(0xF6)= 0x100-0xF6=0x0A – совпадает с /n – заканчиваем посылку контрольной суммой. |
| 17 | `VerPlatPic` | `VerPlatPic` | 0x00C | 1 |  | _VerPlatPic=0x0C – версия платы с процессором Ver Plat Pic = EE_VerPlatPic |
| 18 | `VerPlatNet` | `VerPlatNet` | 0x00D | 1 |  | _VerPlatNet=0x0D – версия сетевой платы Ver Plat NET = (EE_VerPlatNET&7)+6, 4-й бит отвечает за тип сетевой платы: |
| 19 | `VerPlatPowDop` | `VerPlatPowDop` | 0x00E | 1 |  | _VerPlatPowDop=0x0E - данные о версии Pow платы – бит1=0 Low плата, бит1=1 Hi плата, 0x80 – Hybrid (гибрид) |
| 20 | `VerTest` | `VerTest` | 0x00F | 1 |  | _VerTest=0x0F - Под версия PO для тестирования, 0 - релиз. |
| 21 | `ErrD_I2C_verPO4_MPPT` | `ErrD_I2C_verPO4_MPPT` | 0x010 | 1 |  | Бит4: ErrD_I2C_verPO4_MPPT=0x10 – устаревшая версия ПO МППТ (<4.0). |
| 22 | `xF0` | `XF0` | 0x010 | 1 |  | где S=0x100-mod256(0x72+0x0F+0xDB+0xDC+0xDB+0xDD)=0x100-mod256(0x3F0)=0x100-0xF0=0x10 |
| 23 | `TFT_CIChargeSOC95` | `TFT_CIChargeSOC95` | 0x013 | 1 |  | _TFT_CIChargeSOC95=0x13 {5} – Ток заряда при достижении емкости АКБ 95%. Активна только для модульных АКБ со связью с МАП. |
| 24 | `fUAcc_Korr` | `FUAcc_Korr` | 0x016 | 1 |  | _fUAcc_Korr=0x16 - Поправка датчика напряжения АКБ в десятых вольта приведенные к 12В. |
| 25 | `SerialNum0` | `SerialNum0` | 0x018 | 1 |  | _SerialNum0=0x18 – номер младший байт |
| 26 | `SerialNum1` | `SerialNum1` | 0x019 | 1 |  | _SerialNum1=0x19 – номер старший байт |
| 27 | `T_ButCh_r` | `T_ButCh_r` | 0x01A | 1 |  | _T_ButCh_r=0x1A {64} - время в секундах*4 - время на реакцию топливного генератора т.е. на его включение и прогрев, если нет реакции выключаем генератор. Если включена соотв. оп… |
| 28 | `T_Diz_prof` | `T_Diz_prof` | 0x01B | 1 |  | _T_Diz_prof=0x1B {75} - время в секундах*4 - время на включение топливного генератора на профилактику. Если включена соотв. опция в доп. реле. |
| 29 | `POW_Korr` | `POW_Korr` | 0x01C | 1 |  | _POW_Korr=0x1C - Коррекция мощности. Значение ячейки содержит отношение реальной мощности к измеренной датчиком и умноженное на 128. Т.е. если EE_POW_Korr=128 коррекции нет. |
| 30 | `I_Chage_Korr` | `I_Chage_Korr` | 0x01D | 1 |  | _I_Chage_Korr=0x1D - Коррекция тока во время заряда. Значение ячейки содержит отношение реального тока к измеренной датчиком и умноженное на 128. Т.е. если EE_I_Chage_Korr =128 … |
| 31 | `TempTopToBMS` | `TempTopToBMS` | 0x021 | 1 |  | _BMS=0x44C – Ячейка для работы с BMS. Либо можно читать состояние BMS подключенного по сухим контактам, либо управлять непосредственно через RS232. Режим работы BMS задается яче… |
| 32 | `SerialNum2` | `SerialNum2` | 0x022 | 1 |  | _SerialNum2=0x22 – зарезервирован (0) |
| 33 | `SerialNum3` | `SerialNum3` | 0x023 | 1 |  | _SerialNum3=0x23 – индекс серийного номера (буква ASCII). |
| 34 | `SyncDiz_Plat` | `SyncDiz_Plat` | 0x024 | 1 |  | _SyncDiz_Plat=0x24 |
| 35 | `No3FazeMotor` | `No3FazeMotor` | 0x025 | 1 | T | _No3FazeMotor=0x25 1бит: 0 - если разрешена коррекция синусоид 3-х фаз для асинхронных Моторов (по умолчанию). Также пункт меню Титанатор. |
| 36 | `T_AccDisch_ExtH` | `T_AccDisch_ExtH` | 0x026 | 1 |  | _T_AccDisch_ExtH=0x26 - верхний байт времени работы при полной разрядке в секундах, для расширения времени более чем _T_AccDisch т.е. если надо сделать время работы на полном ра… |
| 37 | `TFT_SOC_DisCharge` | `TFT_SOC_DisCharge` | 0x02C | 1 |  | _TFT_SOC_DisCharge=0x2C - выключение по полному разряду по SOC. |
| 38 | `TFT_SOC_StartCharge` | `TFT_SOC_StartCharge` | 0x02D | 1 |  | _TFT_SOC_StartCharge=0x2D - Старт заряда по SOC |
| 39 | `TFT_SOC_DizStart` | `TFT_SOC_DizStart` | 0x02E | 1 |  | _TFT_SOC_DizStart=0x2E - Старт дизель-генератора по SOC |
| 40 | `PNET_Prodag_MAX` | `PNET_Prodag_MAX` | 0x033 | 1 |  | _PNET_Prodag_MAX=0x33 – максимальная мощность на продажу. |
| 41 | `DigitInFunc1` | `DigitInFunc1` | 0x034 | 1 |  | _DigitInFunc1=0x34 {0} - 0-нет, 1- кнопка старт, 2- кнопка заряд, 3- реле. Назначение на цифровой вход1 от внешней доп. платы ExtBut. |
| 42 | `DigitInFunc2` | `DigitInFunc2` | 0x035 | 1 |  | _DigitInFunc2=0x35 {0} - 0-нет, 1- кнопка старт, 2- кнопка заряд, 3- реле. Назначение на цифровой вход2 от внешней доп. платы ExtBut. |
| 43 | `AVR_On` | `AVR_On` | 0x03E | 1 | T | _AVR_On=0x3E - Включает через доп. реле режим АВР – автоматизированная работа с топливным генератором, у которого есть соответствующие контакты управления. Подробнее см. доп. Ре… |
| 44 | `Grid_52Hz` | `Grid_52Hz` | 0x03F | 1 |  | _Grid_52Hz=0x3F - 0- нет управления сетевым солнечным инвертором, 1- включаем управление сетевым солнечным инвертором, 2- включаем управление сетевым солнечным инвертором но с у… |
| 45 | `ErrS_TorPolarity` | `ErrS_TorPolarity` | 0x040 | 1 |  | Бит6 ErrS_TorPolarity=0x40 - не правильная полярность подключения тора |
| 46 | `CulerErr` | `CulerErr` | 0x048 | 1 |  | Бит3 - изменились ячейки накопители ошибок с _CulerErr=0x48 до 0x4f (прописывает МАП). |
| 47 | `MODE` | `MODE` | 0x050 | 1 |  | Бит1 - изменился режим работы - записали ячейку по адресу _MODE=0x50. |
| 48 | `MASK_DEV_ON` | `MASK_DEV_ON` | 0x051 | 1 |  | _MASK_DEV_ON=0x51 - Маски на включенные компоненты – отсутствует в модели TITANATOR (т.к. включены всегда). |
| 49 | `NUMCULLER` | `NUMCULLER` | 0x052 | 1 |  | _NUMCULLER=0x52 - кол-во опрашиваемых вентиляторов - разное в зависимости от мощности прибора. |
| 50 | `WATCH` | `WATCH` | 0x053 | 2 |  | _WATCH=0x53-0x54 - Внутреннее время. Биты: 0-4 – день месяца(5 бит); 5-8 – месяц года (4 бита); 9-14 – год столетия (6 бит) до 2063 года; 15 бит в резерве. Часы и десятки минут … |
| 51 | `WATCH2` | `WATCH2` | 0x055 | 1 |  | _WATCH2=0x55 - Внутреннее время, резерв. |
| 52 | `UAccAfterDisChage` | `UAccAfterDisChage` | 0x05B | 1 |  | _UAccAfterDisChage=0x5B – Напряжение, с которого МАП начнет генерацию, после отключения по полному разряду, т.е. после 1минуты работы на напряжении ниже _LCD_UAccMin. |
| 53 | `FrozenUAccAfterDisChage` | `FrozenUAccAfterDisChage` | 0x05D | 1 |  | Также ее можно сделать независимой и следовательно устанавливать в любое значение. Для этого необходимо установить в 1 ячейку _FrozenUAccAfterDisChage=0x5D. |
| 54 | `NetUpECO_net2_off` | `NetUpECO_net2_off` | 0x05F | 1 | T | _NetUpECO_net2_off=0x5F - 1 - отключает подкачку для второго входа сети (но не принудительную генерацию) в ЭКО режимах. Пункт меню Титанатор. |
| 55 | `detTforDopRele` | `DetTforDopRele` | 0x060 | 1 | T | _detTforDopRele=0x60 – датчик температуры от которого будет зависеть переключение внешнего реле настроенного на переключение по температуре (0-1биты Реле1, 2-3биты Реле3, 4-5бит… |
| 56 | `LCD_UAccDizStart_dop` | `LCD_UAccDizStart_dop` | 0x061 | 1 |  | _LCD_UAccDizStart_dop=0x61 - Дополнительная ячейка для увеличения точности установки напряжения _LCD_UAccDizStart=0x153 см. ниже. |
| 57 | `RSBAUD_Rasb_Load` | `RSBAUD_Rasb_Load` | 0x064 | 1 |  | _RSBAUD_Rasb_Load=0x64 {4\|(6<<4)} – нижние 4 бита (по умолчанию 4 - 115200 бод) для работы RS232 для малины; верхние 4 бита (по умолчанию 6 - 625000 бод) для загрузки через PC. |
| 58 | `CIChargeAbsorb` | `CIChargeAbsorb` | 0x065 | 1 | T | _CIChargeAbsorb=0x65 – Минимальный ток заряда в режиме дозаряда (после которого дозаряд прекращается) в процентах от выставленной емкости АКБ. Также пункт меню Титанатор. |
| 59 | `T_MaxLiIonBMS` | `T_MaxLiIonBMS` | 0x066 | 1 |  | _T_MaxLiIonBMS=0x66 – Время работы в дозаряде при активировании BMS. По умолчанию 30 мин. |
| 60 | `del_UAccChBUF_24h` | `Del_UAccChBUF_24h` | 0x067 | 1 |  | _del_UAccChBUF_24h=0x67 – На сколько надо снизить буферное напряжение через 24ч. По умолчанию значение ячейки 5, что соответствует 0,5В/1В/2В/4В для 12/24/48/96 блоков. |
| 61 | `PowAccNom` | `PowAccNom` | 0x068 | 1 |  | _PowAccNom=0x68 - Номинальная мощность в процентах от заявленной максимальной. При привышении этой мощности генерация прекратится через 10-30мин (в зависимости от модели). По ум… |
| 62 | `x94` | `X94` | 0x06C | 1 |  | o 0x00 0x01 0x02 0x03 0x04 0x05 0x06 0x07 0x08 0x09 0xDB 0xDC 0x0B 0x0C 0x0D 0x0E 0x0F 0x6C\n где S=0x100-mod256(0x6F+0x00+0x01+0x02+0x03+0x04+0x05+0x06+0x07+0x08+0x09+0xDB+0xDC… |
| 63 | `NUM_Overload` | `NUM_Overload` | 0x073 | 1 |  | _NUM_Overload=0x73 - Кол-во попыток перегрузки при срабатывании защиты по перегрузкам. |
| 64 | `T_CHARGE` | `T_CHARGE` | 0x078 | 1 |  | _T_CHARGE=0x78 - Задержка в сек перехода на заряд после появления сети и разряженном АКБ. |
| 65 | `T_CHARGE_UmaxECO_diz` | `T_CHARGE_UmaxECO_diz` | 0x079 | 1 |  | _T_CHARGE_UmaxECO_diz=0x79 - Если один из входов сети 220в настроен на топливный генератор или в доп. реле выбран топливный генератор или режим АВР то можно изменить время дозар… |
| 66 | `DelUChargeEnd` | `DelUChargeEnd` | 0x07A | 1 |  | _DelUChargeEnd=0x7A {20} – сдвиг (в вольтах/10) относительно конца заряда вниз для отработки напряжения заряда второй ступени. Напряжение рассчитано для 48 моделей, соотв. сдвиг… |
| 67 | `T_NET_ON` | `T_NET_ON` | 0x07B | 1 |  | _T_NET_ON=0x7B - Время на стабилизацию сети при ее появлении, перед тем как пропустить ее на выход. |
| 68 | `T_CHARGE_Umax` | `T_CHARGE_Umax` | 0x07E | 1 |  | _T_CHARGE_Umax=0x7E - Максимальное время работы в режиме дозаряда в мин. Через это время дозаряд закончится даже если ток заряда будет выше значения выставленного в ячейке _CICh… |
| 69 | `x81` | `X81` | 0x07F | 1 |  | где S=0x100-mod256(0x6F+0x12)=0x100-mod256(0x81)=0x100-0x81=0x7F |
| 70 | `T_ACCDISCH` | `T_ACCDISCH` | 0x080 | 1 |  | _T_AccDisch_ExtH=0x26 - Старший байт времени работы при полной разрядке в секундах, для расширения времени более 255с=~4мин. Используется совместно с _T_ACCDISCH=0x80 см. ниже. |
| 71 | `T_NOMINAL` | `T_NOMINAL` | 0x083 | 1 |  | _T_NOMINAL=0x83 - время работы на нагрузке выше номинальной в минутах (через это время генерация отключится). |
| 72 | `KorrTempAkb` | `KorrTempAkb` | 0x085 | 1 | T | _KorrTempAkb=0x85 – Также пункт меню Титанатор. |
| 73 | `LCD_UNET2Up` | `LCD_UNET2Up` | 0x089 | 1 | T | _LCD_UNET2Up=0x89 - Ограничение напряжения II входа сети сверху, если сетевое напряжение превысит этот порог то МАП перейдет на генерацию от АКБ. Обратный возврат на сеть произо… |
| 74 | `LCD_UNET2Down` | `LCD_UNET2Down` | 0x08A | 1 | T | _LCD_UNET2Down=0x8A - Ограничение напряжения II входа сети с снизу, если сетевое напряжение будет ниже этого порога то МАП перейдет на генерацию от АКБ. Обратный возврат на сеть… |
| 75 | `x6F` | `X6F` | 0x091 | 1 |  | В данном случае контрольная сумма сразу вычисляется S=0x100-mod256(0x6F)=0x100-0x6F=0x91, |
| 76 | `x36` | `X36` | 0x0CA | 1 |  | где S=0x100-mod256(0x77+0x03+0x00+0x3A+0x01+0x02+0x03+0x7C)= 0x100-mod256(0x136)= 0x100-0x36=0xCA, |
| 77 | `x33` | `X33` | 0x0CD | 1 |  | где S=0x100-mod256(0x77+0x00+0x00+0x3A+0x82)= 0x100-mod256(0x133)= 0x100-0x33=0xCD, |
| 78 | `x2E` | `X2E` | 0x0D2 | 1 |  | где S=0x100-mod256(0x72+0x00+0x82+0x3A)= 0x100-mod256(0x12E)= 0x100-0x2E=0xD2, |
| 79 | `ReleNetNum` | `ReleNetNum` | 0x0D7 | 1 |  | _ReleNetNum=0xD7 {b 10 10 10 10} - по 2 бита для каждого доп. реле R4,R3,R2,R1: 2 (b10) если реагируем и по первому и по втрому входу сети, 0 (b00) -по первому, 1 (b01) - по вто… |
| 80 | `ReleIzbNum` | `ReleIzbNum` | 0x0D8 | 1 |  | _ReleIzbNum=0xD8 {0} - по 2 бита для каждого доп реле R4,R2,R3,R1: 0- 1-й избыток, 1 - 2-й избыток, 2 - 3-й избыток, 3 - 4-й избыток. Например, b 11 10 01 00 – первое реле настр… |
| 81 | `TCh_MPPT_GRID52` | `TCh_MPPT_GRID52` | 0x0DA | 1 | T | _TCh_MPPT_GRID52=0xDA {24} - в 2часах 2д*24ч=48 -> 48/2ч=24, 0- выключено. Пункт меню Титанатор. |
| 82 | `DataRS` | `DataRS` | 0x0DB | 1 |  | if(buf[j_buf]==0xDD) DataRS=0xDB; |
| 83 | `TchEco_14day` | `TchEco_14day` | 0x0DC | 1 |  | _TchEco_14day=0xDC {168} - в 2часах 14д*24ч=336 -> 336/2ч=168, 0- выключено – запланированный заряд через X дней. 14 дней по умолчанию. |
| 84 | `minTarifAnCh` | `MinTarifAnCh` | 0x0DE | 1 |  | _minTarifAnCh=0xDE – |
| 85 | `minTarifCh` | `MinTarifCh` | 0x0DF | 1 |  | _minTarifCh=0xDF – |
| 86 | `CAP_OnOff` | `CAP_OnOff` | 0x0F6 | 1 |  | _CAP_OnOff=0xF6 - Включение САП-Заряд. Включается режим управления САП, по проводу DSR на разъеме RS232, осуществляется управление САП-ом на включение/выключение дизель-генерато… |
| 87 | `P_ModemVdd` | `P_ModemVdd` | 0x0FE | 1 |  | _P_ModemVdd=0xFE - Включение питания +12В на разъем МОДЕМА, – отсутствует в модели TITANATOR. |
| 88 | `sum` | `Sum` | 0x0FF | 1 |  | sum=0xff-sum; sum++; put_char(sum);//запись контрольной суммы |
| 89 | `BMS_Num` | `BMS_Num` | 0x100 | 1 | T | _BMS_Num=0x100 - кол-во ячеек BMS, (для более ранних версий ПО определялись как 4/8/16 для моделей 12/24/48v соответственно). Также пункт меню Титанатор. |
| 90 | `S` | `S` | 0x100 | 1 |  | Т.е. S=0x100-mod256(сумма кроме \n и самой контрольной суммы) |
| 91 | `LCD_CAcc_dop` | `LCD_CAcc_dop` | 0x101 | 1 |  | _LCD_CAcc_dop=0x101 |
| 92 | `LCD_UAccMin_dop` | `LCD_UAccMin_dop` | 0x102 | 1 |  | _LCD_UAccMin_dop=0x102 |
| 93 | `LCD_UAccMinNetGen_dop` | `LCD_UAccMinNetGen_dop` | 0x103 | 1 |  | _LCD_UAccMinNetGen_dop=0x103 |
| 94 | `LCD_UAccChMax_dop` | `LCD_UAccChMax_dop` | 0x104 | 1 |  | _LCD_UAccChMax_dop=0x104 |
| 95 | `LCD_UAccChBUF_dop` | `LCD_UAccChBUF_dop` | 0x105 | 1 |  | _LCD_UAccChBUF_dop=0x105 |
| 96 | `LCD_UAccChStart_dop` | `LCD_UAccChStart_dop` | 0x106 | 1 |  | _LCD_UAccChStart_dop=0x106 |
| 97 | `LCD_NetMaxPow_dop` | `LCD_NetMaxPow_dop` | 0x107 | 1 |  | _LCD_NetMaxPow_dop=0x107 |
| 98 | `LCD_TypeSin` | `LCD_TypeSin` | 0x138 | 1 |  | Бит2 - изменили настройки МАП - ячейки с _LCD_TypeSin=0x138 до _LCDmax_T_MAXCharge=0x1CF - Эти ячейки мог поменять пользователь с помощью дисплея ЖКИ (в этом случае этот бит ука… |
| 99 | `LCD_MAP_iFaze` | `LCD_MAP_iFaze` | 0x139 | 1 |  | _LCD_MAP_iFaze=0x139 – Только для моделей с дополнительной платой расширения или DOMINATOR/Титанатор. |
| 100 | `LCD_UMAP220_NEED` | `LCD_UMAP220_NEED` | 0x13A | 1 |  | _LCD_UMAP220_NEED=0x13A - Выходное напряжение которое стабилизирует МАП на выходе (реальное напряжение может быть ниже на максимальных мощностях и/или сильно разряженных АКБ) |
| 101 | `LCD_NetUpLoad` | `LCD_NetUpLoad` | 0x13B | 1 |  | _LCD_NetUpLoad=0x13B - Только для моделей Hybrid или DOMINATOR/Титанатор. |
| 102 | `LCD_NetUpECO` | `LCD_NetUpECO` | 0x13C | 1 |  | _LCD_NetUpECO=0x13C - Подкачка по ЭКО. Только для моделей Hybrid или DOMINATOR/Титанатор. |
| 103 | `LCD_UAccMin` | `LCD_UAccMin` | 0x13D | 1 |  | _UAcc_Optim_VL=0x520, _UAcc_Optim_VH=0x521 – Среднее напряжение между измеренным средним напряжением АКБ (_UAcc_med_VH, _UAcc_med_VL) и напряжением АКБ которое не учитывает нагр… |
| 104 | `LCD_SensLoad` | `LCD_SensLoad` | 0x13E | 1 |  | _LCD_SensLoad=0x13E - Включение режима в котором генерация от АКБ будет происходить только при подключенной нагрузке. |
| 105 | `LCD_MAP_Sync` | `LCD_MAP_Sync` | 0x13F | 1 |  | _LCD_MAP_Sync=0x13F - Только для моделей с дополнительной платой. |
| 106 | `LCD_NetDizel` | `LCD_NetDizel` | 0x150 | 1 |  | _LCD_NetDizel=0x150 - Указывает кто является источником сети на основном входе. Под дизель-генератором подразумевается любая мини-электростанция в том числе на газу или бензине. |
| 107 | `LCD_Net2` | `LCD_Net2` | 0x151 | 1 |  | _LCD_Net2=0x151 – В модели DOMINATOR/Титанатор есть второй вход для напряжения 220В. |
| 108 | `LCD_DizelMaxPow` | `LCD_DizelMaxPow` | 0x152 | 1 |  | _LCD_DizelMaxPow=0x152 – Установка максимальной мощности для второго входа для модели DOMINATOR/Титанатор. |
| 109 | `LCD_UAccDizStart` | `LCD_UAccDizStart` | 0x153 | 1 |  | Альтернатива ячейке _LCD_UAccDizStart=0x153 - пуск топливного генератора по напряжению АКБ. Эти пункты активными становятся при работе с доп. Реле. |
| 110 | `LCD_SlaveMAPNum` | `LCD_SlaveMAPNum` | 0x155 | 1 |  | Все ведущие и фазные имеют возможность соединения с ведомыми (параллельными) для наращивания мощности. Кол-во ведомых задается в ячейке _LCD_SlaveMAPNum=0x155 см. ниже. |
| 111 | `LCD_BMSFunk` | `LCD_BMSFunk` | 0x156 | 1 |  | _LCD_BMSFunk=0x156 – Подключение внешних BMS и/или MPPT. |
| 112 | `LCD_NetMaxPow` | `LCD_NetMaxPow` | 0x168 | 1 |  | _LCD_NetMaxPow=0x168 – Максимальная мощность сетевого входа. По умолчанию выставлена максимальной которую МАП может через себя пропустить. Пользователь может ее установить ниже … |
| 113 | `LCD_UNETUp` | `LCD_UNETUp` | 0x169 | 1 |  | _LCD_UNETUp=0x169 - Ограничение напряжение сети сверху, если сетевое напряжение превысит этот порог то МАП перейдет на генерацию от АКБ. Обратный возврат на сеть произойдет как … |
| 114 | `LCD_UNETDown` | `LCD_UNETDown` | 0x16A | 1 |  | _LCD_UNETDown=0x16A - Ограничение напряжение сети с снизу, если сетевое напряжение будет ниже этого порога то МАП перейдет на генерацию от АКБ. Обратный возврат на сеть произойд… |
| 115 | `LCD_NETAlg` | `LCD_NETAlg` | 0x16B | 1 |  | _LCD_NETAlg=0x16B - Алгоритм работы с сетью. |
| 116 | `LCD_UAccMinNetGen` | `LCD_UAccMinNetGen` | 0x16C | 1 |  | _LCD_UAccMinNetGen=0x16C - Напряжение меньше которого в ЭКО принудительная генерация (или подкачка) заканчивается и переходим на сеть. Обратный возврат на принудительную генерац… |
| 117 | `LCD_TimeEcoStart` | `LCD_TimeEcoStart` | 0x16D | 1 |  | _LCD_TimeEcoStart=0x16D – Время начала минимального тарифа с точностью до 10 мин. В старших 5 битах хранятся часы, в младших 3 битах десятки минут. |
| 118 | `LCD_TimeEcoEnd` | `LCD_TimeEcoEnd` | 0x16E | 1 |  | _LCD_TimeEcoEnd=0x16E - Время окончания минимального тарифа с точностью до 10мин, аналогично в старших 5 битах хранятся часы, в младших 3 битах десятки минут. |
| 119 | `LCD_PercentECOMinGen` | `LCD_PercentECOMinGen` | 0x16F | 1 |  | _LCD_PercentECOMinGen=0x16F – Только для моделей Hybrid или DOMINATOR. |
| 120 | `LCD_AccType` | `LCD_AccType` | 0x180 | 1 |  | _LCD_AccType=0x180 - Тип АКБ. Могут добавляться по мере увеличения номенклатуры. |
| 121 | `LCD_CAcc` | `LCD_CAcc` | 0x181 | 1 |  | _LCD_CAcc=0x181 - Емкость АКБ. В этой ячейке хранится так называемая “полная емкость” АКБ, сделано это для совместимости со старыми моделями. |
| 122 | `LCD_CIChargeStart` | `LCD_CIChargeStart` | 0x182 | 1 |  | _TFT_CIChStartMPPT {10} – Ток первой ступени, аналогична ячейке _LCD_CIChargeStart=0x182 только для тока от КЭС(MPPT). |
| 123 | `LCD_CIChargeEnd` | `LCD_CIChargeEnd` | 0x183 | 1 |  | _TFT_CIChEndMPPT {5} - Ток второй ступени, аналогична ячейке _LCD_CIChargeEnd=0x183 только для тока от КЭС(MPPT). |
| 124 | `LCD_ChargeAlg` | `LCD_ChargeAlg` | 0x184 | 1 |  | _LCD_ChargeAlg=0x184 - Алгоритм заряда. |
| 125 | `LCD_UAccChMax` | `LCD_UAccChMax` | 0x185 | 1 | T | _DelUChargeEnd=0x7A - сдвиг напряжения относительно конца заряда (вниз) для отработки перехода на вторую ступень заряда. Этот сдвиг напряжения рассчитан под 48в модель титанатор… |
| 126 | `LCD_UAccChBUF` | `LCD_UAccChBUF` | 0x186 | 1 |  | _LCD_UAccChBUF=0x186 - Напряжение буферного используется совместно с _LCD_UAccChBUF_dop и корректируется от датчика температуры АКБ. |
| 127 | `LCD_UAccChStart` | `LCD_UAccChStart` | 0x187 | 1 |  | Альтернатива пункта "Uакб Старт Заряда:" т.е ячейки _LCD_UAccChStart=0x187. Ниже этого значения МАП ВКЛючит режим заряда аналогично пункту "Uакб Старт Заряда:". |
| 128 | `LCD_Rele1_Funk` | `LCD_Rele1_Funk` | 0x198 | 1 |  | _LCD_Rele1_Funk=0x198 – РЕЛЕ1 можно запрограммировать на пуск Бензо-Дизель генератора, а также можно включать РЕЛЕ по условию. Как правило, это нужно для включения/ отключения н… |
| 129 | `LCD_Rele1_U_T_min` | `LCD_Rele1_U_T_min` | 0x199 | 1 |  | _LCD_Rele1_U_T_min=0x199 – Зависит от выбора пункта “Функция Реле1” т.е. ячейки _LCD_Rele1_Funk. |
| 130 | `LCD_Rele1_U_T_maxInv` | `LCD_Rele1_U_T_maxInv` | 0x19A | 1 |  | _LCD_Rele1_U_T_maxInv=0x19A – Зависит от выбора пункта “Функция Реле1” т.е. ячейки _LCD_Rele1_Funk. |
| 131 | `LCD_Rele1_GistInv` | `LCD_Rele1_GistInv` | 0x19B | 1 |  | _LCD_Rele1_GistInv=0x19B – Зависит от выбора пункта “Функция Реле1” т.е. ячейки _LCD_Rele1_Funk. |
| 132 | `LCD_Rele2_Funk` | `LCD_Rele2_Funk` | 0x19C | 1 |  | _LCD_Rele2_Funk=0x19C – Аналогично _LCD_Rele1_Funk, только для Реле2. |
| 133 | `LCD_Rele2_U_T_min` | `LCD_Rele2_U_T_min` | 0x19D | 1 |  | _LCD_Rele2_U_T_min=0x19D – Аналогично _LCD_Rele1_U_T_min, только для Реле2 и зависит ячейки _LCD_Rele2_Funk. |
| 134 | `LCD_Rele2_U_T_maxInv` | `LCD_Rele2_U_T_maxInv` | 0x19E | 1 |  | _LCD_Rele2_U_T_maxInv=0x19E – Аналогично _LCD_Rele1_U_T_mах, только для Реле2 и зависит ячейки _LCD_Rele2_Funk. |
| 135 | `LCD_Rele2_GistInv` | `LCD_Rele2_GistInv` | 0x19F | 1 |  | _LCD_Rele2_GistInv=0x19F – Аналогично _LCD_Rele1_GistInv, только для Реле2 и зависит ячейки _LCD_Rele2_Funk. |
| 136 | `LCD_Vers` | `LCD_Vers` | 0x1B0 | 1 |  | _LCD_Vers=0x1B0 - Версия ПО и модели, реально ячейка пустая и не используется, нужна только как пункт меню. |
| 137 | `LCD_LCDType` | `LCD_LCDType` | 0x1B2 | 1 |  | _LCD_LCDType=0x1B2 – Режим подсветки ЖКИ. |
| 138 | `LCD_Sound` | `LCD_Sound` | 0x1B3 | 1 |  | _LCD_Sound=0x1B3 - Подтверждение звуком. |
| 139 | `LCD_SignalNetOff` | `LCD_SignalNetOff` | 0x1B4 | 1 |  | _LCD_SignalNetOff=0x1B4 - Звук на пропажу сети. |
| 140 | `LCD_RSBAUD` | `LCD_RSBAUD` | 0x1B5 | 1 |  | _LCD_RSBAUD=0x1B5 - Скорость работы по RS232 (RS1), Режитм САП, Питание для WiFi. |
| 141 | `E_LCD_TimeCyr` | `E_LCD_TimeCyr` | 0x1B6 | 1 |  | _TimeCyr_MINUT=0x44B – Счетчик минут текущего времени. Счетчик кратен 10 минутам, т.е. значения в ней от 0 до 9, на десятой минуте сбрасывается в 0. Остальные значения (часы и д… |
| 142 | `LCD_TimeCyr` | `LCD_TimeCyr` | 0x1B6 | 1 |  | Бит4 - изменилась ячейка текущего времени _LCD_TimeCyr=0x1B6 (МАП обновляет раз в 10мин). |
| 143 | `LCD_T_MAXCharge` | `LCD_T_MAXCharge` | 0x1B7 | 1 |  | _LCD_T_MAXCharge=0x1B7 - Максимальное время заряда если не установлен буферный режим. |
| 144 | `LCD_RasberyON` | `LCD_RasberyON` | 0x1C9 | 1 |  | _LCD_RasberyON=0x1C9 {1} - 0 - вЫключен, 1- Rasbery включен |
| 145 | `LCD_DevAdr` | `LCD_DevAdr` | 0x1CA | 1 |  | _LCD_DevAdr=0x1CA {1} - номер (адрес) МАП по которому можно адресоваться к МАП, например, по протоколу ModBus. |
| 146 | `LCD_RS_Protocol` | `LCD_RS_Protocol` | 0x1CB | 1 |  | _LCD_RS_Protocol=0x1CB {0} - 0- обычный протокол, 1- ModBus RTU (USB), 2- ModBus RTU (RS485) т.е транслируем RS1 на RS485 c спротоколом ModBus. |
| 147 | `LCD_AddDevice` | `LCD_AddDevice` | 0x1CC | 1 |  | _LCD_AddDevice=0x1CC {0} - 0- нет устройств на доп. плате, 1- RS485(232)_Sl для протокола Slave, 2 -Vektor485_BMS(ModBus) Master, 3 - Seplos485_BMS Master, 4 - Dyness485_ BMS Ma… |
| 148 | `LCD_RSBAUD485` | `LCD_RSBAUD485` | 0x1CD | 1 |  | _LCD_RSBAUD485=0x1CD {4} - 0- 2400, 1- 9600, 2 - 19200, 3 - 57600, 4 - 115200, 5 - 312500, 6- 625000 скорость в бод для RS3. |
| 149 | `LCD_RS485_Num` | `LCD_RS485_Num` | 0x1CE | 1 |  | _LCD_RS485_Num=0x1CE {1} - Кол-во устройств подключаемых к RS3 по ModBus. |
| 150 | `LCDmax_T_MAXCharge` | `LCDmax_T_MAXCharge` | 0x1CF | 1 |  | Бит2 - изменили настройки МАП - ячейки с _LCD_TypeSin=0x138 до _LCDmax_T_MAXCharge=0x1CF - Эти ячейки мог поменять пользователь с помощью дисплея ЖКИ (в этом случае этот бит ука… |
| 151 | `StatusCh` | `StatusCh` | 0x402 | 1 |  | _StatusCh=0x402 - Показывает состояние заряда в котором находится МАП. |
| 152 | `put_eeprom` | `Put_eeprom` | 0x403 | 1 |  | В памяти RAM находятся данные меняющиеся в процессе работы типа напряжения, ток и т.д. и которые, как правило, нужно отслеживать в реальном времени. В EEPROM находятся в основно… |
| 153 | `StateUAcc` | `StateUAcc` | 0x404 | 1 |  | _StateUAcc=0x404 - Состояние напряжения на АКБ. |
| 154 | `UAcc_med_VH` | `UAcc_med_VH` | 0x405 | 1 |  | _UAcc_med_VH=0x405, _UAcc_med_VL=0x406 - Напряжение АКБ. |
| 155 | `UAcc_med_VL` | `UAcc_med_VL` | 0x406 | 1 |  | _UAcc_med_VH=0x405, _UAcc_med_VL=0x406 - Напряжение АКБ. |
| 156 | `IAcc_med_A_2` | `IAcc_med_A_2` | 0x408 | 1 |  | _IAcc_med_A_2=0x408 - Ток по АКБ при генерации или ток заряда (или при трансляции сети в режиме подкачки в сеть). |
| 157 | `PLoad_L` | `PLoad_L` | 0x409 | 1 |  | _PLoad_L=0x409 - текущая мощность нагрузки по АКБ т.е. при генерации (или при трансляции сети в режиме подкачки в сеть). Данная ячейка отображает мощность потребления блоков мощ… |
| 158 | `T_NetOn` | `T_NetOn` | 0x40B | 1 |  | _T_NetOn=0x40B - обратный отсчет задержки переключения с генерации на сеть при ее появлении, при NetOn=0 и наличии сети произойдет переключение на трансляцию сети. |
| 159 | `T_Charge` | `T_Charge` | 0x40C | 1 |  | _T_Charge=0x40C – обратный отсчет времени для перехода на заряд, при T_Charge=0 начнется заряд. |
| 160 | `T_AccDisch_L` | `T_AccDisch_L` | 0x40E | 1 |  | _T_AccDisch_L=0x40E – счетчик, обратного отсчета времени работы в режиме генерации на напряжении АКБ ниже минимально допустимого, при T_AccDisch=0 генерация выключается. Ячейка … |
| 161 | `T_Nominal` | `T_Nominal` | 0x40F | 1 |  | _T_Nominal=0x40F - счетчик времени работы на нагрузке выше номинальной в минутах (через 30мин генерация отключится). |
| 162 | `T_Overload` | `T_Overload` | 0x410 | 1 |  | _T_Overload=0x410 – обратный отсчет времени работы на перегрузке (нагрузка выше максимальной мощности) или другой критической ситуации (КЗ, низкое напряжение), при T_Overload=0 … |
| 163 | `TOff_Overload` | `TOff_Overload` | 0x411 | 1 |  | _TOff_Overload=0x411 – обратный отсчет времени простоя после перегрузки или другой критической ситуации (КЗ, низкое напряжение), при TOff_Overload=0 генерация возобновится. |
| 164 | `F_AccOver` | `F_AccOver` | 0x41C | 1 |  | _F_AccOver=0x41C - Показывает состояние возникающее при перегрузках (генерация приостанавливается). |
| 165 | `F_NETOver` | `F_NETOver` | 0x41D | 1 |  | _F_NETOver=0x41D - Показывает состояние возникающее при перегрузках по сети 220В. |
| 166 | `UNET` | `UNET` | 0x422 | 1 |  | _UNET=0x422 - Напряжение сети. |
| 167 | `INET` | `INET` | 0x423 | 1 |  | _INET=0x423 - Ток отбираемый от сети, в том числе на заряд. |
| 168 | `PNET_L` | `PNET_L` | 0x424 | 1 |  | _PNET_L=0x424 - Мощность отбираемая от сети, в том числе на заряд. Данная ячейка отображает мощность потребления блоков мощностью до 25кВт. Для блоков большей мощности необходим… |
| 169 | `TFNET` | `TFNET` | 0x425 | 1 |  | _TFNET=0x425 - Частота напряжения в сети. |
| 170 | `ThFMAP` | `ThFMAP` | 0x426 | 1 |  | _ThFMAP=0x426 - Частота напряжения на выходе во время генерации. |
| 171 | `UOUTmed` | `UOUTmed` | 0x427 | 1 |  | _UOUTmed=0x427 – Напряжение на выходе во время генерации |
| 172 | `TFNET_Limit` | `TFNET_Limit` | 0x428 | 1 |  | _TFNET_Limit=0x428 – Частота вышла за пределы – переходим с сети на генерацию, значение этой ячейки сохраняет значение этой частоты Flimit(Гц)=6250/TFNET_Limit для версий ПО >= … |
| 173 | `UNET_Limit` | `UNET_Limit` | 0x429 | 1 |  | _UNET_Limit=0x429 – Напряжение вышло за пределы – переходим с сети на генерацию, значение этой ячейки сохраняет значение этого напряжения Ulimit(В)=UNET_Limit+100. |
| 174 | `RSErrSis` | `RSErrSis` | 0x42A | 1 |  | _RSErrSis=0x42A - Ошибки системы критические - произойдет выключение - необходимо исправить МАП, в нормальной работе должно быть RSErrSis=0. |
| 175 | `RSErrJobM` | `RSErrJobM` | 0x42B | 1 |  | _RSErrJobM=0x42B - ошибки (перегрузки) - после устранения выход в рабочий режим (отображается на ЖКИ). |
| 176 | `RSErrJob` | `RSErrJob` | 0x42C | 1 |  | _RSErrJob=0x42C - ошибки (перегрузки) - после устранения выход в рабочий режим (отображается на ЖКИ). |
| 177 | `RSWarning` | `RSWarning` | 0x42D | 1 |  | _RSWarning=0x42D - предупреждения рабочие - не меняют ход работы (отображается на ЖКИ). |
| 178 | `Temp_Grad0` | `Temp_Grad0` | 0x42E | 1 |  | _Temp_Grad0=0x42E - Температура от внешнего датчика температуры (который наклеен на АКБ). |
| 179 | `Temp_Grad1` | `Temp_Grad1` | 0x42F | 1 |  | _Temp_Grad1=0x42F - Температура датчика тора (отсутствовал в старых моделях) |
| 180 | `Temp_Grad2` | `Temp_Grad2` | 0x430 | 1 |  | _Temp_Grad2=0x430 - Температура датчика температуры транзисторов |
| 181 | `INET_16_4` | `INET_16_4` | 0x431 | 1 |  | _INET_16_4=0x431 - Более точное значение тока сети (аналогичное _INET), для подсчета статистики. |
| 182 | `IAcc_med_A_u16_L` | `IAcc_med_A_u16_L` | 0x432 | 1 |  | _IAcc_med_A_u16_L=0x432, _IAcc_med_A_u16_H=0x433 - Более точное значение тока по АКБ (аналогичное _IAcc_med_A_2), для подсчета статистики. |
| 183 | `IAcc_med_A_u16_H` | `IAcc_med_A_u16_H` | 0x433 | 1 |  | _IAcc_med_A_u16_L=0x432, _IAcc_med_A_u16_H=0x433 - Более точное значение тока по АКБ (аналогичное _IAcc_med_A_2), для подсчета статистики. |
| 184 | `RCON_img` | `RCON_img` | 0x439 | 1 |  | _RCON_img=0x439 – Служебная ячейка из которой можно узнать по какой причине произошел сброс микроконтроллера. |
| 185 | `RCON_img_L` | `RCON_img_L` | 0x439 | 1 |  | _RCON_img_L=0x439 – Служебная ячейка из которой можно узнать по какой причине произошел сброс микроконтроллера. |
| 186 | `Temp_off` | `Temp_off` | 0x43C | 1 |  | _Temp_off=0x43C - для того чтобы знать какие есть датчики температуры. |
| 187 | `E_LCD_UAccChMax` | `E_LCD_UAccChMax` | 0x43F | 1 |  | _E_LCD_UAccChMax=0x43F - Текущее значение максимального напряжения заряда (конца заряда) поправленное по температуре внешнего датчика. |
| 188 | `E_LCD_UAccChBUF` | `E_LCD_UAccChBUF` | 0x440 | 1 |  | _E_LCD_UAccChBUF=0x440 - Текущее значение буферного (поддерживающего) напряжения заряда поправленное по температуре внешнего датчика. |
| 189 | `T_CHARGE_NEED_ECO` | `T_CHARGE_NEED_ECO` | 0x441 | 1 |  | _T_CHARGE_NEED_ECO=0x441 - Счетчик, в минутах, задержки перехода из ЭКО режима (режим принудительной генерации при напряжении ниже EE_LCD_UAccMin) для игнорирования просадки. |
| 190 | `RCON_img_H` | `RCON_img_H` | 0x446 | 1 |  | _RCON_img_H=0x446 |
| 191 | `RSErrDop` | `RSErrDop` | 0x447 | 1 |  | _RSErrDop=0x447 – Ошибки трехфазной системы, параллельных МАП, BMS и MPPT. |
| 192 | `ThFMAPSync` | `ThFMAPSync` | 0x449 | 1 |  | _ThFMAPSync=0x449 – Частота предыдущей фазы (если текущая фаза в режиме генерации, то ее частота станет равной частоте предыдущей фазе). |
| 193 | `TimeCyr_MINUT` | `TimeCyr_MINUT` | 0x44B | 1 |  | _TimeCyr_MINUT=0x44B – Счетчик минут текущего времени. Счетчик кратен 10 минутам, т.е. значения в ней от 0 до 9, на десятой минуте сбрасывается в 0. Остальные значения (часы и д… |
| 194 | `BMS` | `BMS` | 0x44C | 1 |  | _BMS=0x44C – Ячейка для работы с BMS. Либо можно читать состояние BMS подключенного по сухим контактам, либо управлять непосредственно через RS232. Режим работы BMS задается яче… |
| 195 | `M_POWhourNET_L` | `M_POWhourNET_L` | 0x44D | 1 |  | _M_POWhourNET_L=0x44D, _M_POWhourNET_H=0x44E, _M_POWhourNET_HH=0x44F, |
| 196 | `M_POWhourNET_x` | `M_POWhourNET_x` | 0x44D | 3 |  | К примеру, счетчик _M_POWhourNET_x=0x44d-0x44f считает не учитывая знак тока. Т.е. НЕ вычитает энергию при “продаже” в сеть, а суммирует ее. Счетчик M_POWhourNET_sign учитывает … |
| 197 | `M_POWhourNET_H` | `M_POWhourNET_H` | 0x44E | 1 |  | _M_POWhourNET_L=0x44D, _M_POWhourNET_H=0x44E, _M_POWhourNET_HH=0x44F, |
| 198 | `M_POWhourNET_HH` | `M_POWhourNET_HH` | 0x44F | 1 |  | _M_POWhourNET_L=0x44D, _M_POWhourNET_H=0x44E, _M_POWhourNET_HH=0x44F, |
| 199 | `M_POWhourMAP_L` | `M_POWhourMAP_L` | 0x450 | 1 |  | _M_POWhourMAP_L=0x450, _M_POWhourMAP_H=0x451, _M_POWhourMAP_HH=0x452, |
| 200 | `M_POWhourMAP_H` | `M_POWhourMAP_H` | 0x451 | 1 |  | _M_POWhourMAP_L=0x450, _M_POWhourMAP_H=0x451, _M_POWhourMAP_HH=0x452, |
| 201 | `M_POWhourMAP_HH` | `M_POWhourMAP_HH` | 0x452 | 1 |  | _M_POWhourMAP_L=0x450, _M_POWhourMAP_H=0x451, _M_POWhourMAP_HH=0x452, |
| 202 | `M_POWhourMAPCharge_L` | `M_POWhourMAPCharge_L` | 0x453 | 1 |  | _M_POWhourMAPCharge_L=0x453, _M_POWhourMAPCharge_H=0x454, _M_POWhourMAPCharge_HH=0x455, |
| 203 | `M_POWhourMAPCharge_H` | `M_POWhourMAPCharge_H` | 0x454 | 1 |  | _M_POWhourMAPCharge_L=0x453, _M_POWhourMAPCharge_H=0x454, _M_POWhourMAPCharge_HH=0x455, |
| 204 | `M_POWhourMAPCharge_HH` | `M_POWhourMAPCharge_HH` | 0x455 | 1 |  | _M_POWhourMAPCharge_L=0x453, _M_POWhourMAPCharge_H=0x454, _M_POWhourMAPCharge_HH=0x455, |
| 205 | `PLoad_H` | `PLoad_H` | 0x456 | 1 |  | _PLoad_H=0x456 – дополнительный байт для отображения мощности нагрузки по АКБ (или при трансляции сети в режиме подкачки в сеть) для блоков мощностью более 25кВт. |
| 206 | `PNET_H` | `PNET_H` | 0x457 | 1 |  | _PNET_H=0x457 - дополнительный байт для отображения мощности отбираемой от сети, в том числе на заряд для |
| 207 | `CulerSpead` | `CulerSpead` | 0x458 | 1 |  | _CulerSpead=0x458 – текущая скорость вращения вентилятора: |
| 208 | `fl_UAccChBUF_24h` | `Fl_UAccChBUF_24h` | 0x516 | 1 |  | fl_UAccChBUF_24h=0x516 – флаг возникновения события “24ч”. |
| 209 | `UAcc_Optim_V` | `UAcc_Optim_V` | 0x520 | 1 |  | Но за выключение по полному разряду отвечает ячейка _UAcc_Optim_V=0x520 см. ниже. |
| 210 | `UAcc_Optim_VL` | `UAcc_Optim_VL` | 0x520 | 1 |  | _UAcc_Optim_VL=0x520, _UAcc_Optim_VH=0x521 – Среднее напряжение между измеренным средним напряжением АКБ (_UAcc_med_VH, _UAcc_med_VL) и напряжением АКБ которое не учитывает нагр… |
| 211 | `UAcc_Optim_VH` | `UAcc_Optim_VH` | 0x521 | 1 |  | _UAcc_Optim_VL=0x520, _UAcc_Optim_VH=0x521 – Среднее напряжение между измеренным средним напряжением АКБ (_UAcc_med_VH, _UAcc_med_VL) и напряжением АКБ которое не учитывает нагр… |
| 212 | `UAcc_med_VS_100L` | `UAcc_med_VS_100L` | 0x522 | 1 |  | _UAcc_med_VS_100L=0x522, _UAcc_med_VS_100H=0x523 – Альтернатива ячейкам_UAcc_med_VH, _UAcc_med_VL. |
| 213 | `UAcc_med_VS_100H` | `UAcc_med_VS_100H` | 0x523 | 1 |  | _UAcc_med_VS_100L=0x522, _UAcc_med_VS_100H=0x523 – Альтернатива ячейкам_UAcc_med_VH, _UAcc_med_VL. |
| 214 | `MPPT_toCh` | `MPPT_toCh` | 0x524 | 1 |  | _MPPT_toCh=0x524 – Состояние MPPT контроллера(ов). Можно использовать для принудительного перевода MPPT в разные режимы: |
| 215 | `flagUnet2` | `FlagUnet2` | 0x527 | 1 |  | _flagUnet2=0x527 – Показывает состояние первого и второго входа 220В и какой вход активный в данный момент (если включен режим двух входов) только в моделях с двумя входами сети… |
| 216 | `I_Akb_MAP_faz1L` | `I_Akb_MAP_faz1L` | 0x528 | 1 |  | _I_Akb_MAP_faz1L=0x528, _I_Akb_MAP_faz1H=0x529 - ток фаз по АКБ с точностью 0,1А, при отрицательных токах (при заряде) добавляется верхний бит 0x80 в _I_Akb_MAP_faz1H. |
| 217 | `I_Akb_MAP_faz1H` | `I_Akb_MAP_faz1H` | 0x529 | 1 |  | _I_Akb_MAP_faz1L=0x528, _I_Akb_MAP_faz1H=0x529 - ток фаз по АКБ с точностью 0,1А, при отрицательных токах (при заряде) добавляется верхний бит 0x80 в _I_Akb_MAP_faz1H. |
| 218 | `I_Akb_MAP_faz2L` | `I_Akb_MAP_faz2L` | 0x52A | 1 |  | _I_Akb_MAP_faz2L=0x52A, _I_Akb_MAP_faz2H=0x52B - тоже для фазы2 |
| 219 | `I_Akb_MAP_faz2H` | `I_Akb_MAP_faz2H` | 0x52B | 1 |  | _I_Akb_MAP_faz2L=0x52A, _I_Akb_MAP_faz2H=0x52B - тоже для фазы2 |
| 220 | `I_Akb_MAP_faz3L` | `I_Akb_MAP_faz3L` | 0x52C | 1 |  | _I_Akb_MAP_faz3L=0x52C, _I_Akb_MAP_faz3H=0x52D - тоже для фазы3 |
| 221 | `I_Akb_MAP_faz3H` | `I_Akb_MAP_faz3H` | 0x52D | 1 |  | _I_Akb_MAP_faz3L=0x52C, _I_Akb_MAP_faz3H=0x52D - тоже для фазы3 |
| 222 | `T_AccDisch` | `T_AccDisch` | 0x52E | 1 |  | _T_AccDisch_L=0x40E – счетчик, обратного отсчета времени работы в режиме генерации на напряжении АКБ ниже минимально допустимого, при T_AccDisch=0 генерация выключается. Ячейка … |
| 223 | `TdizProfilact_day_cnt` | `TdizProfilact_day_cnt` | 0x582 | 1 |  | _TdizProfilact_day_cnt=0x582 – счетчик времени для включения топливного генератора на 5-ти минутную провилактику. См. ячейку _TdizProfilact_day в EEProm задающую время для этого… |
| 224 | `TchEco_Ch` | `TchEco_Ch` | 0x583 | 1 |  | _TchEco_Ch=0x583 - счетчик времени полного заряда в часах*2 (т.е. прибавляем каждые два часа). Если напряжение АКБ не достигало напряжения полного заряда в течении _TchEco_14day… |
| 225 | `flag_NETON_ECO` | `Flag_NETON_ECO` | 0x585 | 1 |  | _flag_NETON_ECO=0x585 - флаг ЭКО режима или тарифной сети. |
| 226 | `Status_RELEdop` | `Status_RELEdop` | 0x586 | 1 |  | _Status_RELEdop=0x586 |
| 227 | `PNET_Sign_P` | `PNET_Sign_P` | 0x587 | 1 |  | _PNET_Sign_P=0x587 |
| 228 | `LCD_UAccChMax_S_Temp` | `LCD_UAccChMax_S_Temp` | 0x588 | 2 |  | _LCD_UAccChMax_S_Temp=0x588-0x589 - Напряжение окончания заряда c коррекцией по температуре - полное умноженное на 10. |
| 229 | `Pmax_On` | `Pmax_On` | 0x58C | 1 |  | _Pmax_On=0x58C - случился Pmax, т.е. нагрузка превысила выставленную максимальную мощность сети. |
| 230 | `PmaxGenDisch` | `PmaxGenDisch` | 0x58D | 1 |  | _PmaxGenDisch=0x58D - случился полный разряд по Pmax, переходим на сеть независимо от выставленной максимальной мощности сети. |
| 231 | `M_POWhourNET_sign` | `M_POWhourNET_sign` | 0x592 | 1 |  | См. также ячейку _M_POWhourNET_sign=0x592 учитывающую знак тока сети. |
| 232 | `PNET_8` | `PNET_8` | 0x59A | 2 |  | _PNET_8=0x59A-0x59B - текущая мощность сети в киловатах/10 и *8, знак определяется по PNET_Sign_P=0x587. |
| 233 | `PLoad_8` | `PLoad_8` | 0x59E | 2 |  | _PLoad_8=0x59E-0x59F - текущая мощность нагрузки в киловатах/10 и *8 |
| 234 | `UAccMinNetGen_S` | `UAccMinNetGen_S` | 0x5DC | 2 |  | _ UAccMinNetGen_S=0x5DC-0x5DD - Реальное напряжение ЭКО которое в отличии от соответствующей ячейки EEProm может быть скорректировано по Uбуф. |
| 235 | `SoC` | `SoC` | 0x718 | 2 | T | _SoC=0x718-0x719 – Текущая емкость батареи в % от полной емкости батареи (с учетом SoH). Т.е. насколько разрядилась батарея под нагрузкой на данный момент. Параметр передается о… |
| 236 | `SoH` | `SoH` | 0x71A | 2 | T | _SoH=0x71A-0x71B – Текущая полная емкость батареи в % от указанной в паспорте на батарею. Т.е. указывает насколько батарея потеряла свою полную емкость на текущий момент. Параме… |
