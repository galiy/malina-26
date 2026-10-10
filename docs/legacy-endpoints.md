# Legacy API — инвентаризация (что есть и у нас не реализовано)

Полный список **не-HTML** эндпоинтов старой «Малины» (бэкап
`titanator_malina_fixed_20261007`, раздел `settings/html`), которые у нас **пока не
реализованы**. HTML-страницы не включены.

Уже перенесено (см. `docs/legacy-api.md`): `read_json.php`, `read_memory.php`,
`check_daemons.php`, `write_eeprom.php`.

Исторически многие — тонкие обёртки над `shell_exec(...)`/файлами; ответы
разнородны (текст, HTML-строки таблиц, JSON, `redirect` на referer).

## Система, сеть, службы

| Эндпоинт | Запрос | Ответ | Эффект / смысл |
|---|---|---|---|
| `get_cpu_mem.php` | GET | JSON-массив | CPU/RAM/uptime (`cpu_ram_uptime.sh`) |
| `get_lan_state.php` | GET | текст (`\n`→`:`) | состояние LAN (`get_lan_connection.sh`) |
| `get_wlan_state.php` | GET | текст (`\n`→`:`) | состояние WLAN |
| `eth_set.php` | POST `ip_mode_en,IP_addr_en,IP_mask_en,IP_dgw_en,eth_dns1,eth_dns2` | redirect на referer | настройки Ethernet (статик/DHCP) |
| `wifi_set.php` | POST `wifi_mode,ESSID,password,ip_mode,IP_addr,IP_mask,IP_dgw,wifi_dns1,wifi_dns2,encoding,pair` | redirect | настройка Wi‑Fi клиента |
| `hostname_set.php` | POST `hostname` | redirect | смена hostname |
| `time_set.php` | POST `datetime,zone` | redirect | установка даты/времени и TZ |
| `switch_lang.php` | GET | пусто | переключение языка интерфейса |
| `reboot_do.php` | POST | текст + `Refresh:2` | перезагрузка |
| `shutdown_do.php` | POST | текст + `Refresh:2` | выключение |
| `service_control.php` | GET `action(start/stop/restart/reset),service(mapd/mpptd/chargerd/emd/dbed/batmond)` | пусто/`-1` | управление демонами |
| `devices.php` | GET | текст | список устройств (`get_devices.sh`) |
| `find_devices.php` | POST | пусто | поиск устройств (`systemctl start add_tty`) |
| `errors_counter.php` | GET `device=map` | число | строк в `map_errors.log` |

## Обновление / бэкап / БД

| Эндпоинт | Запрос | Ответ | Эффект |
|---|---|---|---|
| `check_update.php` | POST | `1`/`0` | флаг завершённого обновления (`/var/map/update_done`) |
| `update_main.php` | POST | пусто | запуск `main_script.sh` |
| `update_start.php` | POST | redirect | старт обновления |
| `usb_backup.php` | POST | `-1` если нет USB‑HDD | бэкап на USB |
| `get_usbhd_info.php` | GET | JSON | инфо USB‑HDD (наличие, размеры day/month БД) |
| `db_backup.php` | POST | пусто | бэкап БД (`backup_db.sh &`) |
| `db_restore.php` | POST | строка цифр (`1/0` наличие day/month), `-1` при несоответствии | восстановление БД |
| `get_db_status.php` | GET `action(backup/restore)` | `doing/success/fail/unknown` | статус |
| `clear_events.php` | POST | пусто | очистка журнала событий |

## МАП / MPPT / BMS (данные и история)

| Эндпоинт | Запрос | Ответ | Эффект |
|---|---|---|---|
| `get_data.php` | POST `device,field,time,number,date` | JSON (или `-1`) | история значения поля из БД (графики) |
| `get_map_e.php` | GET `counter(1..5)` | JSON; `-1` вне диапазона | энергосчётчики МАП (часть — из `batmon.log`) |
| `get_mppt_e.php` | GET `mppt(0..3)` | JSON; `-1` | энергия MPPT (`mpptN_energy.json`) |
| `map_errors.php` | POST | HTML | текущие ошибки МАП |
| `map_errors_history.php` | POST `start_date,end_date` | HTML-строки | история ошибок МАП |
| `map_counters_history.php` | POST `start_date,end_date` | HTML-строки | история счётчиков МАП |
| `mppt_errors.php` | GET `mppt` | HTML | ошибки MPPT |
| `mppt_errors_history.php` | POST `device,start_date,end_date` | HTML-строки | история ошибок MPPT |
| `mppt_counters_history.php` | POST `device,start_date,end_date` | HTML-строки | история счётчиков MPPT |
| `batmon_history.php` | POST `start_date,end_date` | HTML-строки | история АКБ |
| `read_bms.php` | GET | JSON `{"updated":…,"devices":[…]}` | ANT BMS из shm (демон `bmslistener`) |

## Узлы, меню, настройки МАП/АКБ

| Эндпоинт | Запрос | Ответ | Эффект |
|---|---|---|---|
| `load_menu.php` | GET | JSON | меню/параметры МАП (из shm 1996) |
| `get_nodes.php` | GET | JSON | список узлов (`nodes.list`) |
| `master_node_load.php` | POST | JSON / `failed` | загрузка конфигурации master‑node |
| `master_node_save.php` | POST `data` | `1` / `failed` | сохранение master‑node |
| `load_ab_set.php` | POST | JSON / `error` | загрузка настроек АКБ (`battery.json`) |
| `save_ab_set.php` | POST `data` | `Ok` / `-1` | сохранение настроек АКБ |
| `wind_mppts_set.php` | POST `numbers` | `ok` / `failed` | настройка wind‑MPPT (`wind_mppts`) + рестарт mapd |
| `events_get.php` | GET | JSON / `failed` | список событий (`events.list`) |
| `events_set.php` | POST `data` | пусто / `failed` | сохранение событий |
| `email_set.php` | POST `smtp_server,port,smtp_login,email_password,email_sender,email_recipient,email_subject,email_text,email_auth,tls_mode,starttls_mode` | redirect | настройки SMTP |
| `email_test.php` | POST | вывод `test_email.sh` | тестовое письмо |

## Облако / Emoncms

| Эндпоинт | Запрос | Ответ | Эффект |
|---|---|---|---|
| `cloud_switch.php` | POST | пусто | вкл/выкл облако (`mac_collector`/`mac_sender`) |
| `get_cloud_enable.php` | GET | `1`/`0` | флаг включённости облака |
| `get_cloud_status.php` | GET | `1`/`0` | статус процессов облака |
| `set_cloud_settings.php` | POST `key` | `ok`/`failed` | сохранение API‑ключа (`api_key`) |
| `emoncms_switch.php` | POST | пусто/`-1` | вкл/выкл Emoncms |
| `get_emoncms_enable.php` | GET | `1`/`0` | флаг Emoncms |
| `get_emoncms_status.php` | GET | `1`/`0` | статус процесса |
| `get_emoncms_settings.php` | GET | JSON / `failed` | настройки (`emoncms.json`) |
| `set_emoncms_settings.php` | POST `data` | `ok`/`failed` | сохранение настроек Emoncms |

## Служебные (shm/sec/batmon) и прокси

| Эндпоинт | Запрос | Ответ | Эффект |
|---|---|---|---|
| `read_sec.php` | GET `id,offset` | данные / `error` | «безопасное» чтение по id (mapd/mppt) |
| `write_sec.php` | POST | `error`/… | «безопасная» запись (через mapd) |
| `write_shm.php` | GET `target=uc1…` | `error` / число | запись управляющих байтов в shm |
| `get_eeprom_state.php` | GET | `error`/`reload`/`no_changes` | чтение/сброс флага состояния EEPROM в shm |
| `batmon_100.php` | POST | пусто | SIGUSR2 batmond (перечитать 100 %) |
| `batmon_reset.php` | POST | пусто | сброс данных batmon (`rm /settings/batmon/*`, стоп) |
| `request_node.php` | POST `url,dev,id,port` | `<id>\n<answer>` | прокси: запрос к другому узлу (`read_json.php?device=map/bat/mppt`, `check_daemons.php`) |

## Приоритет для нас

Многие пункты относятся к подсистемам, которых у нас пока **нет** (MPPT/КЭС,
batmon, ANT BMS, Emoncms, облако, БД/история, USB‑бэкап, апдейтер) — это не
«забытый API», а иной функционал.

Из нашего профиля (МАП + система) реально не хватает:
`request_node.php` (прокси‑агрегатор), `service_control`, `reboot_do`/`shutdown_do`,
`devices`/`find_devices`, `get_cpu_mem`, `load_menu`,
`get_eeprom_state`/`write_shm`.
