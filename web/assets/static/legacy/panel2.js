// Панель 2 «Рефакторинг»: копия legacy-панели, но данные НЕ запрашивает сама.
// Родитель присылает бинарный контракт (ArrayBuffer) + событие обновления;
// здесь он парсится и legacy-поля пересчитываются по правилам legacy api.
(function () {
  "use strict";

  var power = [1.3, 1.5, 2, 3, 4.5, 6, 9, 12, 15, 18, 24, 36];
  var voltage = [12, 24, 48, 96];
  var map_mode = ["МАП выключен и нет сети на входе", "МАП выключен. Сеть на входе", "МАП включен. Генерация от АКБ. Нет сети.", "МАП включен и транслирует сеть", "МАП включен. Трансляция + заряд", "", "", "", "", "",
    "Принудительная генерация", "Тарифная сеть. максимальный тариф. принудительная генерация", "Тарифная сеть. минимальный тариф",
    "Трансляция + эко-подкачка", "Трансляция + продажа в сеть", "Ожидание внешнего полного заряда", "Тарифная сеть. трансляция+эко-подкачка",
    "Тарифная сеть. трансляция+продажа в сеть", "Подкачка Pmax"];
  var loc = { 'V': 'В', 'W': 'Вт', 'kW': 'кВт', 'kWh': 'кВтч', 'hz': 'Гц', 'h': 'ч',
    'title1': 'Uвх,В', 'title2': 'Iвх,A', 'title3': 'Uвых,В', 'title5': 'Uакб,В',
    'title6': 'Iакб-мап,А', 'title6_1': 'Imppts', 'title8': 'В' };

  // Список полей (контракт позиционный) — берём у родителя (same-origin), без handshake.
  function getFields() {
    try { if (window.MAP_FIELDS && window.MAP_FIELDS.length) { return window.MAP_FIELDS; } } catch (err) { /* ignore */ }
    try { if (parent && parent.MAP_FIELDS && parent.MAP_FIELDS.length) { return parent.MAP_FIELDS; } } catch (err) { /* ignore */ }
    return [];
  }
  var e = {};
  var inited = false;
  var cfg = { Pow: 0, Uacc: 0, IFaze: 0, NetUpECO: 0, BMSFunk: 0, VerPO: 0, MPPTNum: 0, BMSNum: 0 };
  var map_fw = 0, max_power = 48000, max_mppt_power = 5000, mppt_power_i2c = 0;
  var mppt_mode = ['i', 'v', 'B', 'b'];
  var map_mppt = [], mppt_div = [];
  var bms_div = [], bms_pb = [];
  var map = {};
  var map_balance = [[0, 0], [1, 0], [2, 0], [3, 0], [4, 0], [5, 0], [6, 0], [7, 0], [8, 0], [9, 0]];
  var ab_balance = [[0, 0], [1, 0], [2, 0], [3, 0], [4, 0], [5, 0], [6, 0], [7, 0], [8, 0], [9, 0]];
  var avg_ab = 0, avg_map = 0, counter = 1, plot_ab = null, plot_map = null;

  function addClass(x, c) { if (x && !x.hasClass(c)) { x.addClass(c); } }
  function removeClass(x, c) { if (x && x.hasClass(c)) { x.removeClass(c); } }
  function d1(x) { return Math.round(x * 10) / 10; } // как «%.1f» в legacy api

  // --- парсинг бинарного контракта (как map.js) ---
  function parse(buf) {
    var b = new Uint8Array(buf), dv = new DataView(buf), i;
    if (b.length < 64 || dv.getUint32(0, true) !== 0x4441504d) { return null; }
    var F = getFields();
    if (!F.length) { return null; }
    var n = dv.getUint16(6, true), blen = Math.ceil(n / 8), vbase = 64 + blen;
    var f = {};
    for (i = 0; i < n; i++) {
      if (!(b[64 + (i >> 3)] & (1 << (i % 8)))) { continue; }
      f[F[i] || ("field_" + i)] = dv.getUint32(vbase + i * 4, true);
    }
    return f;
  }
  function u(f, name) { var v = f[name]; return (typeof v === "number") ? v : 0; }
  function s32(v) { return v | 0; }
  function u24(f, l, h, hh) { return u(f, l) + u(f, h) * 256 + u(f, hh) * 65536; }

  function modeCode(M, N, E, U, L, P, V) {
    if (typeof M !== "number") { return undefined; }
    if (M <= 1) { return M; } if (M > 3) { return M; }
    if (U === 0) {
      if (L === 1 && (P & 2) && V > 100) { return 18; }
      if (M === 2) { if (V > 100) { if (N === 2) { return 10; } if (N === 3 && (E & 2) === 0) { return 11; } if (N === 3) { return 12; } } return 2; }
      return 3;
    }
    if (M === 3) {
      if (U === 1) { if (N === 2) { return (E & 1) ? 15 : 13; } if (N === 3) { return (E & 2) ? 12 : 16; } }
      if (U === 2) { if (N === 2) { return (E & 1) ? 15 : 14; } if (N === 3) { return (E & 2) ? 12 : 17; } }
    }
    return M;
  }

  // --- пересчёт legacy-полей из контракта ---
  function compute(f) {
    var sh = u(f, "UACC") & 0x1f;
    var m = {};
    m['_Uacc'] = (u(f, "UAcc_med_VH") * 256 + u(f, "UAcc_med_VL")) / 10;
    m['_Uch_T'] = (u(f, "E_LCD_UAccChMax") << sh) / 10;
    m['_Ubuf_T'] = (u(f, "E_LCD_UAccChBUF") << sh) / 10;

    var unetRaw = u(f, "UNET");
    m['_UNET'] = unetRaw ? unetRaw + 100 : 0;

    // Режим считаем раньше — от него зависит знак тока АКБ (как в mapd).
    m['_MODE'] = modeCode(u(f, "MODE_RAM"), u(f, "LCD_NETAlg"), u(f, "flag_NETON_ECO"),
      u(f, "LCD_NetUpECO"), u(f, "LCD_NetUpLoad"), u(f, "Pmax_On"), m['_UNET']);

    // Ток АКБ — точно как mapd: (L + H*256)/16, знак по режиму заряда (MODE==4 → «+»).
    m['_IAcc_med_A_u16'] = (u(f, "IAcc_med_A_u16_L") + u(f, "IAcc_med_A_u16_H") * 256) / 16;
    if (m['_MODE'] !== 4) { m['_IAcc_med_A_u16'] = -m['_IAcc_med_A_u16']; }
    var uoutRaw = u(f, "UOUTmed");
    m['_UOUTmed'] = uoutRaw ? uoutRaw + 100 : 0;
    m['_INET_16_4'] = u(f, "INET_16_4") / (u(f, "INET") >= 16 ? 4 : 16);
    m['_PNET_calc'] = m['_UNET'] * m['_INET_16_4'] * (u(f, "PNET_Sign_P") === 0 ? -1 : 1);

    m['_TFNET'] = u(f, "TFNET") ? 6250 / u(f, "TFNET") : 0;
    m['_ThFMAP'] = u(f, "ThFMAP") ? 6250 / u(f, "ThFMAP") : 0;
    m['_PLoad_calc'] = m['_Uacc'] * m['_IAcc_med_A_u16'];

    m['_Temp_Grad0'] = u(f, "Temp_Grad0") - 50;
    m['_Temp_Grad2'] = u(f, "Temp_Grad2") - 50;

    // Сырые счётчики, как в legacy-api/mapd (без деления на 100).
    m['_E_NET'] = u24(f, "M_POWhourNET_L", "M_POWhourNET_H", "M_POWhourNET_HH");
    m['_E_ACC'] = u24(f, "M_POWhourMAP_L", "M_POWhourMAP_H", "M_POWhourMAP_HH");
    m['_E_ACC_CHARGE'] = u24(f, "M_POWhourMAPCharge_L", "M_POWhourMAPCharge_H", "M_POWhourMAPCharge_HH");
    m['_E_NET_SIGN'] = s32(u(f, "M_POWhourNET_sign"));

    function ph(l, h) { var v = (u(f, l) & 0xff) | ((u(f, h) & 0x7f) << 8); return (u(f, h) & 0x80 ? -v : v) / 10; }
    m['_I_ph1'] = ph("I_Akb_MAP_faz1L", "I_Akb_MAP_faz1H");
    m['_I_ph2'] = ph("I_Akb_MAP_faz2L", "I_Akb_MAP_faz2H");
    m['_I_ph3'] = ph("I_Akb_MAP_faz3L", "I_Akb_MAP_faz3H");
    m['_I_acc_3ph'] = m['_I_ph1'] + m['_I_ph2'] + m['_I_ph3'];

    m['_flagUnet2'] = u(f, "flagUnet2");
    m['_CoolerSpeed'] = u(f, "CulerSpead");
    var rele = u(f, "Status_RELEdop");
    m['_Relay1'] = rele & 1; m['_Relay2'] = (rele >> 1) & 1;
    m['_fl_UAccChBUF_24h'] = u(f, "fl_UAccChBUF_24h_45A");

    m['_ErrCount'] =
      ((u(f, "RSErrSis") + u(f, "RSErrJobM") + u(f, "RSErrJob") + u(f, "RSWarning") + u(f, "RSErrDop") +
        ((u(f, "RSErrDop") >> 2) & 7) + u(f, "F_Net_Over") + u(f, "F_Acc_Over")) > 0) ? 1 : 0;

    // КЭС из контракта (как legacy-api): количество LCD_MPPTNum, гейт LCD_BMSFunk ∈ {2,3}.
    var kes = [];
    var bmsf = u(f, "LCD_BMSFunk"), kn = u(f, "LCD_MPPTNum");
    if ((bmsf === 2 || bmsf === 3) && kn >= 1 && kn <= 16) {
      for (var ki = 0; ki < kn; ki++) {
        var kcur = ((u(f, "I_Akb_MPPT_L" + ki) & 0xff) | ((u(f, "I_Akb_MPPT_H" + ki) & 0x7f) << 8)) / 16;
        kes.push({ I: kcur, T: u(f, "Takb_MPPT" + ki) - 50 });
      }
    }
    var kavg = 0;
    if (kes.length) { var ksum = 0; for (var kj = 0; kj < kes.length; kj++) { ksum += kes[kj].I; } kavg = ksum / kes.length; }
    m['_kes'] = kes;
    m['_I_mppt_avg'] = kavg;
    m['_P_mppt_avg'] = Math.round(m['_Uacc'] * kavg);
    m['_MPPTs_mode'] = 0;

    // BMS из контракта (как legacy-api): количество BMS_Num, гейт LCD_BMSFunk ∈ {1,3}.
    var bms = [];
    var bn = u(f, "BMS_Num");
    if ((bmsf === 1 || bmsf === 3) && bn >= 1 && bn <= 32) {
      for (var bi2 = 0; bi2 < bn; bi2++) {
        var bv = ((u(f, "UakbCell_L" + bi2) & 0xff) | ((u(f, "UakbCell_H" + bi2) & 0x7f) << 8)) / 100;
        var raw = u(f, "TakbCell" + bi2);
        var bt = (raw === 255) ? 127 : (raw - 50);
        var bq = u(f, "Q_Cell" + bi2);
        bms.push({ V: bv, t: bt, I: bq / 100 * bv });
      }
    }
    m['_bms'] = bms;
    return m;
  }

  function initGauges() {
    var max_current = Math.round(power[cfg.Pow] * 1500 / voltage[cfg.Uacc]);
    if (!isFinite(max_current) || max_current <= 0) { max_current = 50; }
    $('.map_vin').kumaGauge({ value: 220, gaugeWidth: 15, radius: 50, paddingY: 20, paddingX: 2, min: 120, max: 320, fill: '0-#FF4F00:0-#00FF7F:50-#FF4F00:100', showNeedle: false, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title1'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_vout').kumaGauge({ value: 220, gaugeWidth: 15, radius: 50, paddingY: 20, paddingX: 2, min: 120, max: 320, fill: '0-#FF4F00:0-#00FF7F:50-#FF4F00:100', showNeedle: false, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title3'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_inet').kumaGauge({ value: 0, gaugeWidth: 15, radius: 50, fill: '0-#008000:0-#8CD2E7:50-#FF0000:100', paddingY: 20, paddingX: 2, min: -max_current, max: max_current, showNeedle: true, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title2'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_uab').kumaGauge({ value: 0, gaugeWidth: 15, radius: 50, paddingY: 20, paddingX: 2, min: Math.round(voltage[cfg.Uacc] / 1.3), max: Math.round(voltage[cfg.Uacc] * 1.3), fill: '0-#FF4F00:0-#00FF7F:50-#FF4F00:100', showNeedle: false, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title5'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_ibat').kumaGauge({ value: 0, gaugeWidth: 15, radius: 50, fill: '0-#FF0000:0-#8cd2e7:50:#008000:100', paddingY: 20, paddingX: 2, min: -max_current, max: max_current, showNeedle: true, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title6'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_ialt').kumaGauge({ value: 0, gaugeWidth: 15, radius: 50, paddingY: 20, paddingX: 2, min: 0, max: max_current, fill: '#34C924', gaugeBackground: '#bde5f1', showNeedle: false, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title6_1'], fontSize: 20, fontColor: '#3E5F8A' } });
  }

  // Таблица КЭС по числу cfg.MPPTNum (аналог рендера PHP в оригинале).
  function initMppt2() {
    if (cfg.BMSFunk != 2 && cfg.BMSFunk != 3) { $('#mppt_link_wrap').hide(); return; }
    var n = cfg.MPPTNum;
    if (!(n > 1)) { $('#mppt_link_wrap').hide(); return; }
    var html = "<table class='table table-bordered'><tr class='bms' style='background-color:#def0de;'>";
    for (var i = 1; i <= n; i++) {
      html += "<td id='mppt_no" + i + "' style='border-color:#5CB85C !important;'><div style='float:left;'><span style='background-color:#f9f9f9; font-size:130%;font-weight:bolder;'>" + i + "</span></div>" +
        "<div class='progress' style='height:15px !important; max-width:200px;'><div class='progress-bar progress-bar-success' role='progressbar' aria-valuenow='0' aria-valuemin='0' aria-valuemax='100' style='width:0%; color:#13293c' id='mppt_pb" + i + "'></div></div></td>";
    }
    html += "</tr></table>";
    $('#mppt_table').html(html);
    mppt_div = [];
    for (var j = 0; j < n; j++) { mppt_div[j] = $('#mppt_pb' + (j + 1)); }
    $('#mppt_link_wrap').show();
  }

  // Таблица BMS по числу cfg.BMSNum (аналог рендера PHP в оригинале).
  function initBms2() {
    if (cfg.BMSFunk != 1 && cfg.BMSFunk != 3) { $('#bms_link_wrap').hide(); return; }
    var n = cfg.BMSNum;
    if (!(n > 0)) { $('#bms_link_wrap').hide(); return; }
    var head = "<table class='table table-condensed' style='table-layout: fixed;'><thead><tr class='bms bms_header'>";
    var body = "</tr></thead><tbody><tr class='bms'>";
    for (var i = 1; i <= n; i++) {
      head += "<th id='bms_h" + i + "'>BMS " + i + "</th>";
      body += "<td><div id='bms" + i + "' style='display:inline-block'></div>" +
        "<div class='progress' style='height:5px !important;'><div class='progress-bar' role='progressbar' aria-valuenow='0' aria-valuemin='0' aria-valuemax='100' style='width:0%; height:5px;' id='bms_pb" + i + "'></div></div></td>";
    }
    $('#bms_table').html(head + body + "</tr></tbody></table>");
    bms_div = []; bms_pb = [];
    for (var j = 0; j < n; j++) { bms_div[j] = $('#bms' + (j + 1)); bms_pb[j] = $('#bms_pb' + (j + 1)); }
    $('#bms_link_wrap').show();
  }

  function update_charts() {
    var x = 9, min_y = 0, max_y = 0, i;
    if (!isNaN(avg_map) && plot_map) {
      map_balance.splice(0, 1); map_balance.push([x, Math.round(avg_map / 5)]);
      for (i = 0; i < 10; i++) { map_balance[i][0] = i; if (map_balance[i][1] > max_y) max_y = map_balance[i][1] * 1.2; if (map_balance[i][1] < min_y) min_y = map_balance[i][1] * 1.2; if (min_y > 0) min_y = 0; }
      plot_map.destroy();
      plot_map = $.jqplot('chartdiv_map', [map_balance], { seriesDefaults: { showMarker: false, color: '#428bca', shadow: false, renderer: $.jqplot.BarRenderer, rendererOptions: { barWidth: 4, fillToZero: true } }, axes: { xaxis: { showTicks: false }, yaxis: { showTicks: false, min: min_y, max: max_y, numberTicks: 3 } }, highlighter: { show: true, sizeAdjust: 5, tooltipAxes: 'y' } });
    }
    avg_map = 0; max_y = 0;
    if (!isNaN(avg_ab) && plot_ab) {
      ab_balance.splice(0, 1); ab_balance.push([x, Math.round(avg_ab / 5)]);
      for (i = 0; i < 10; i++) { ab_balance[i][0] = i; if (Math.abs(ab_balance[i][1]) > max_y) max_y = Math.abs(ab_balance[i][1]) * 1.2; min_y = -max_y; }
      if (max_y != 0) { plot_ab.destroy(); plot_ab = $.jqplot('chartdiv_ab', [ab_balance], { seriesDefaults: { showMarker: false, color: '#5cb85c', shadow: false, renderer: $.jqplot.BarRenderer, rendererOptions: { barWidth: 4, fillToZero: true } }, axes: { xaxis: { showTicks: false }, yaxis: { showTicks: false, min: min_y, max: max_y, numberTicks: 3 } }, highlighter: { show: true, sizeAdjust: 5, tooltipAxes: 'y' }, negativeColors: ['#d9534f'] }); }
    }
    avg_ab = 0; max_y = 0;
  }

  function bind() {
    e.map_ac_in1 = $('#map_input1'); e.map_ac_in2 = $('#map_input2');
    e.map_inet = $('.map_inet'); e.map_vin = $('.map_vin'); e.map_vout = $('.map_vout');
    e.map_ubat = $('.map_uab'); e.map_ibat = $('.map_ibat');
    e.map_temp1 = $('#map_temp_1'); e.map_ab_temp = $('#map_ab_temp'); e.map_mode_div = $('#map_mode');
    e.map_enet = $('#map_enet'); e.map_eacc = $('#map_eacc'); e.map_echg = $('#map_echg');
    e.map_time = $('#map_time'); e.map_slider = $('#map_slider'); e.ab_slider = $('#ab_slider'); e.mppt_slider = $('#mppt_slider');
    e.f1 = $('#f1'); e.f2 = $('#f2'); e.map_cooler = $('#map_cooler'); e.map_relay1 = $('#map_relay1'); e.map_relay2 = $('#map_relay2');
    e.ab_ph1 = $('#ab_slider_ph1'); e.ab_ph2 = $('#ab_slider_ph2'); e.ab_ph3 = $('#ab_slider_ph3'); e.ab_ph_all = $('#ab_slider_ph_summ');
    e.last_error = $('#last_error'); e.ubuft = $('#Ubuf_t'); e.uchgt = $('#Uchg_t');
    addClass(e.last_error, 'hidden');
  }

  function apply(f) {
    cfg.Pow = u(f, "POW"); cfg.Uacc = u(f, "UACC"); cfg.IFaze = u(f, "LCD_MAP_iFaze");
    cfg.NetUpECO = u(f, "LCD_NetUpECO"); cfg.BMSFunk = u(f, "LCD_BMSFunk"); cfg.VerPO = u(f, "VerPO"); cfg.MPPTNum = u(f, "LCD_MPPTNum"); cfg.BMSNum = u(f, "BMS_Num");
    map = compute(f);
    max_power = (power[cfg.Pow] || 0) * 1500 || 48000;
    map_fw = parseFloat((cfg.VerPO & 0x1F) + (cfg.VerPO >> 5) / 10);
    var uid = (u(f, "SerialNum0") | (u(f, "SerialNum1") << 8));
    var ul = document.getElementById('map_id_label'); if (ul) { ul.textContent = uid; }

    if (!inited) {
      bind(); initGauges(); initMppt2(); initBms2(); inited = true;
      if (cfg.IFaze != 1) { addClass(e.ab_ph1, 'hidden'); addClass(e.ab_ph2, 'hidden'); addClass(e.ab_ph3, 'hidden'); addClass(e.ab_ph_all, 'hidden'); }
      plot_ab = $.jqplot('chartdiv_ab', [ab_balance], { seriesDefaults: { showMarker: false, color: '#428bca', shadow: false, renderer: $.jqplot.BarRenderer, rendererOptions: { barWidth: 4 } }, axes: { xaxis: { showTicks: false }, yaxis: { showTicks: false } }, cursor: { show: true, tooltipLocation: 'sw' } });
      plot_map = $.jqplot('chartdiv_map', [map_balance], { seriesDefaults: { showMarker: false, color: '#428bca', shadow: false, renderer: $.jqplot.BarRenderer, rendererOptions: { barWidth: 4 } }, axes: { xaxis: { showTicks: false }, yaxis: { showTicks: false } }, cursor: { show: true, tooltipLocation: 'sw' } });
    }

    e.map_vin.kumaGauge('update', { value: d1(map['_UNET']) });
    e.map_vout.kumaGauge('update', { value: d1(map['_UOUTmed']) });
    e.map_inet.kumaGauge('update', { value: d1(map['_INET_16_4']) });
    e.map_ubat.kumaGauge('update', { value: d1(map['_Uacc']) });
    e.map_ibat.kumaGauge('update', { value: d1(map['_IAcc_med_A_u16']) });

    e.map_mode_div.html(map_mode[map['_MODE']]);
    e.map_time.html(new Date().toLocaleTimeString());

    var position = 0;
    if (map['_PNET_calc'] != 0) { position = Math.log(Math.abs(map['_PNET_calc'] / 10)) * 47 / Math.log(max_power * 2); }
    if (map['_PNET_calc'] >= 0) { e.map_slider.css('left', (50 + position) + '%'); }
    if (map['_PNET_calc'] < 0) { e.map_slider.css('left', (50 - position) + '%'); }
    e.map_slider.html(Math.round(map['_PNET_calc']) + ' ' + loc['title8'] + 'A</b>');

    if (!isNaN(map['_TFNET'])) { e.f1.html(map['_TFNET'].toFixed(1) + loc['hz']); }
    if (!isNaN(map['_ThFMAP'])) { e.f2.html(map['_ThFMAP'].toFixed(1) + loc['hz']); }
    if (!isNaN(map['_Temp_Grad2'])) { e.map_temp1.html(map['_Temp_Grad2'] + '&degC'); }
    if (!isNaN(map['_Temp_Grad0'])) { e.map_ab_temp.html(map['_Temp_Grad0'] + '&degC'); }
    if (map_fw >= 25.3 && cfg.NetUpECO === 2) { e.map_enet.html('&nbsp;' + map['_E_NET_SIGN'] + ' ' + loc['kWh']); }
    else { e.map_enet.html('&nbsp;' + map['_E_NET'] + ' ' + loc['kWh']); }
    e.map_eacc.html('&nbsp;&nbsp;' + map['_E_ACC'] + ' ' + loc['kWh']);
    e.map_echg.html('&nbsp;&nbsp;' + map['_E_ACC_CHARGE'] + ' ' + loc['kWh']);

    if (map['_flagUnet2'] & 1) { e.map_ac_in2.css('transform', 'scale(1,1)'); e.map_ac_in1.css('transform', 'scale(0.72,0.72)'); }
    else { e.map_ac_in2.css('transform', 'scale(0.72,0.72)'); e.map_ac_in1.css('transform', 'scale(1,1)'); }
    e.map_ac_in2.css('color', (map['_flagUnet2'] & 4) ? '#5cb85c' : 'lightgray');
    e.map_ac_in1.css('color', (map['_flagUnet2'] & 2) ? '#5cb85c' : 'lightgray');

    if (map['_CoolerSpeed'] > 0) { addClass(e.map_cooler, 'rotating'); } else { removeClass(e.map_cooler, 'rotating'); }
    if (map['_Relay1'] > 0) { e.map_relay1.css('color', 'white').css('background-color', 'darkgreen'); }
    else { e.map_relay1.css('color', '#8c8c8c').css('background-color', 'grey'); }
    if (map['_Relay2'] > 0) { e.map_relay2.css('color', 'white').css('background-color', 'darkgreen'); }
    else { e.map_relay2.css('color', '#8c8c8c').css('background-color', 'grey'); }

    if (map['_Ubuf_T'] < 100) {
      if (parseInt(map['_fl_UAccChBUF_24h']) > 0) { e.ubuft.html("<b>&darr;b(24" + loc['h'] + ")</b>:&thinsp;" + map['_Ubuf_T']); }
      else { e.ubuft.html(map['_Ubuf_T']); }
    }
    if (map['_Uch_T'] < 100) { e.uchgt.html(map['_Uch_T']); }

    if (cfg.IFaze == 1) {
      var p1 = (Math.round(map['_Uacc'] * map['_I_ph1']) / 1000).toFixed(2);
      var p2 = (Math.round(map['_Uacc'] * map['_I_ph2']) / 1000).toFixed(2);
      var p3 = (Math.round(map['_Uacc'] * map['_I_ph3']) / 1000).toFixed(2);
      var pa = (Math.round(map['_Uacc'] * map['_I_acc_3ph']) / 1000).toFixed(2);
      e.ab_ph1.html('&#966;<sub>1</sub>:' + p1 + ' ' + loc['kW']);
      e.ab_ph2.html('&#966;<sub>2</sub>:' + p2 + ' ' + loc['kW']);
      e.ab_ph3.html('&#966;<sub>3</sub>:' + p3 + ' ' + loc['kW']);
      e.ab_ph_all.html('&#931;&#966;:' + pa + ' ' + loc['kW']);
    }

    if (map['_ErrCount']) { removeClass(e.last_error, 'hidden'); }

    position = 0;
    if (map['_PLoad_calc'] != 0) { position = Math.log(Math.abs(map['_PLoad_calc'] / 10)) * 47 / Math.log(max_power * 2); }
    if (map['_PLoad_calc'] >= 0) { e.ab_slider.css('left', (50 + position) + '%'); }
    if (map['_PLoad_calc'] < 0) { e.ab_slider.css('left', (50 - position) + '%'); }
    e.ab_slider.html(Math.round(map['_PLoad_calc']) + ' ' + loc['W']);

    // КЭС — как в панели 3, но значения уже посчитаны из контракта (map['_kes']).
    position = 0;
    mppt_power_i2c = 0;
    if (cfg.BMSFunk == 2 || cfg.BMSFunk == 3) {
      mppt_power_i2c = Math.round(map['_Uacc'] * map['_I_mppt_avg']);
      if (mppt_power_i2c > max_mppt_power) { max_mppt_power = mppt_power_i2c * 1.2; }
      if (mppt_power_i2c > 0) { position = 80 * (Math.log(mppt_power_i2c / 10) / Math.log(max_mppt_power)); }
      e.mppt_slider.css('left', position + '%');
      if (!isNaN(mppt_power_i2c)) { e.mppt_slider.html(mppt_mode[map['_MPPTs_mode']] + ':' + mppt_power_i2c + ' ' + loc['W']); }
    }
    map_mppt = map['_kes'] || [];
    if (map_mppt.length > 0) {
      for (var mi = 0; mi < map_mppt.length; mi++) {
        var mw = 0;
        if (map_mppt[mi]['I'] > 0) { mw = 100 * (Math.log(map_mppt[mi]['I'] / 10) / Math.log(max_mppt_power)); }
        if (mppt_div[mi]) { mppt_div[mi].css('width', mw + '%').html(map_mppt[mi]['I'] + 'A'); }
      }
    }

    // BMS — как в панели 3, но из контракта (map['_bms']).
    var bms = map['_bms'] || [];
    if (bms.length > 0) {
      var min_v = bms[0].V, max_v = 0;
      for (var bi = 0; bi < bms.length; bi++) { if (bms[bi].V < min_v) { min_v = bms[bi].V; } if (bms[bi].V > max_v) { max_v = bms[bi].V; } }
      for (var bj = 0; bj < bms.length; bj++) {
        if (!bms_div[bj] || !bms_div[bj].length) { continue; }
        bms_div[bj].html(bms[bj].V + loc['V'] + '<br>' + ((bms[bj].t == 127) ? '-' : bms[bj].t) + '&degC&thinsp;' + bms[bj].I.toFixed(2) + 'A');
        bms_pb[bj].css('width', (100 * bms[bj].V / 3.5) + '%');
        if (bms[bj].I > 1.5) { bms_pb[bj].css('background', 'yellow'); }
        if (bms[bj].I > 2.8) { bms_pb[bj].css('background', 'red'); }
        if (bms[bj].I < 1.5) { bms_pb[bj].css('background', '#5cb85c'); }
        if (min_v != max_v) {
          if (bms[bj].V == min_v) { $('#bms_h' + (bj + 1)).css('background-color', '#5bc0de'); }
          else if (bms[bj].V == max_v) { $('#bms_h' + (bj + 1)).css('background-color', '#e48683'); }
          else { $('#bms_h' + (bj + 1)).css('background-color', '#0d5637'); }
        }
      }
    }

    if (cfg.IFaze == 1) { avg_ab += Number(map['_I_acc_3ph'] * map['_Uacc']); }
    else { avg_ab += Number(map['_PLoad_calc']); }
    if (isNaN(avg_ab)) { avg_ab = 0; }
    avg_map += Number(map['_PNET_calc']);
    if (isNaN(avg_map)) { avg_map = 0; }
    if (++counter == 6) { counter = 1; update_charts(); }
  }

  window.addEventListener("message", function (ev) {
    if (ev.origin !== location.origin) { return; }
    var d = ev.data;
    if (!d || d.type !== "map-raw") { return; }
    var f = parse(d.buf);
    if (f) { apply(f); }
  });
})();
