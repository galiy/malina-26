// Реверс блока МАП со старой панели (index.php/home.js). Без улучшений.
// Источники: /read_json.php?device=map и /read_memory.php (eeprom).
(function () {
  "use strict";

  var power = [1.3, 1.5, 2, 3, 4.5, 6, 9, 12, 15, 18, 24, 36];
  var voltage = [12, 24, 48, 96];

  var map_mode = ["МАП выключен и нет сети на входе", "МАП выключен. Сеть на входе", "МАП включен. Генерация от АКБ. Нет сети.", "МАП включен и транслирует сеть", "МАП включен. Трансляция + заряд", "", "", "", "", "",
    "Принудительная генерация", "Тарифная сеть. максимальный тариф. принудительная генерация", "Тарифная сеть. минимальный тариф",
    "Трансляция + эко-подкачка", "Трансляция + продажа в сеть", "Ожидание внешнего полного заряда", "Тарифная сеть. трансляция+эко-подкачка",
    "Тарифная сеть. трансляция+продажа в сеть", "Подкачка Pmax"];

  var loc = {
    'V': 'В', 'W': 'Вт', 'kW': 'кВт', 'kWh': 'кВтч', 'hz': 'Гц', 'h': 'ч',
    'title1': 'Uвх,В', 'title2': 'Iвх,A', 'title3': 'Uвых,В', 'title5': 'Uакб,В',
    'title6': 'Iакб-мап,А', 'title6_1': 'Imppts', 'title8': 'В',
    'MAC': 'МАП', 'error': 'ошибка', 'na': 'н/д'
  };

  var maps_number = (window.MAP_ID != null) ? 1 : 0;
  var is_batmon = 1; // у нас нет батарейного монитора

  var eeprom = [];
  var map_fw = 0;
  var max_power = 48000, max_mppt_power = 5000, mppt_power_i2c = 0;
  var errors_total = 0, errors_new = 0;
  var map = {}, map_balance = [[0, 0], [1, 0], [2, 0], [3, 0], [4, 0], [5, 0], [6, 0], [7, 0], [8, 0], [9, 0]],
    ab_balance = [[0, 0], [1, 0], [2, 0], [3, 0], [4, 0], [5, 0], [6, 0], [7, 0], [8, 0], [9, 0]];
  var avg_ab = 0, avg_map = 0, counter = 1;
  var plot_ab = null, plot_map = null;

  var e = {}; // элементы

  function addClass(element, cls) { if (element && !element.hasClass(cls)) { element.addClass(cls); } }
  function removeClass(element, cls) { if (element && element.hasClass(cls)) { element.removeClass(cls); } }
  function get_id(id) { return document.getElementById(id); }

  function initGauges() {
    var max_current = Math.round(power[eeprom[0x05]] * 1500 / voltage[eeprom[0x06]]);
    $('.map_vin').kumaGauge({ value: 220, gaugeWidth: 15, radius: 50, paddingY: 20, paddingX: 2, min: 120, max: 320, fill: '0-#FF4F00:0-#00FF7F:50-#FF4F00:100', showNeedle: false, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title1'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_vout').kumaGauge({ value: 220, gaugeWidth: 15, radius: 50, paddingY: 20, paddingX: 2, min: 120, max: 320, fill: '0-#FF4F00:0-#00FF7F:50-#FF4F00:100', showNeedle: false, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title3'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_inet').kumaGauge({ value: 0, gaugeWidth: 15, radius: 50, fill: '0-#008000:0-#8CD2E7:50-#FF0000:100', paddingY: 20, paddingX: 2, min: -max_current, max: max_current, showNeedle: true, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title2'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_uab').kumaGauge({ value: 0, gaugeWidth: 15, radius: 50, paddingY: 20, paddingX: 2, min: Math.round(voltage[eeprom[0x06]] / 1.3), max: Math.round(voltage[eeprom[0x06]] * 1.3), fill: '0-#FF4F00:0-#00FF7F:50-#FF4F00:100', showNeedle: false, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title5'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_ibat').kumaGauge({ value: 0, gaugeWidth: 15, radius: 50, fill: '0-#FF0000:0-#8cd2e7:50:#008000:100', paddingY: 20, paddingX: 2, min: -max_current, max: max_current, showNeedle: true, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title6'], fontSize: 20, fontColor: '#3E5F8A' } });
    $('.map_ialt').kumaGauge({ value: 0, gaugeWidth: 15, radius: 50, paddingY: 20, paddingX: 2, min: 0, max: max_current, fill: '#34C924', gaugeBackground: '#bde5f1', showNeedle: false, valueLabel: { display: true, fontFamily: 'Arial', fontColor: '#000', fontSize: '20', fontWeight: 'bolder' }, label: { display: false }, title: { display: true, value: loc['title6_1'], fontSize: 20, fontColor: '#3E5F8A' } });
  }

  function update_charts() {
    var x = 9, min_y = 0, max_y = 0, i;
    if (!isNaN(avg_map) && plot_map) {
      map_balance.splice(0, 1);
      map_balance.push([x, Math.round(avg_map / 5)]);
      for (i = 0; i < 10; i++) {
        map_balance[i][0] = i;
        if (map_balance[i][1] > max_y) max_y = map_balance[i][1] * 1.2;
        if (map_balance[i][1] < min_y) min_y = map_balance[i][1] * 1.2;
        if (min_y > 0) min_y = 0;
      }
      plot_map.destroy();
      plot_map = $.jqplot('chartdiv_map', [map_balance], { seriesDefaults: { showMarker: false, color: '#428bca', shadow: false, renderer: $.jqplot.BarRenderer, rendererOptions: { barWidth: 4, fillToZero: true } }, axes: { xaxis: { showTicks: false }, yaxis: { showTicks: false, min: min_y, max: max_y, numberTicks: 3 } }, highlighter: { show: true, sizeAdjust: 5, tooltipAxes: 'y' } });
    }
    avg_map = 0; max_y = 0;
    if (!isNaN(avg_ab) && plot_ab) {
      ab_balance.splice(0, 1);
      ab_balance.push([x, Math.round(avg_ab / 5)]);
      for (i = 0; i < 10; i++) {
        ab_balance[i][0] = i;
        if (Math.abs(ab_balance[i][1]) > max_y) max_y = Math.abs(ab_balance[i][1]) * 1.2;
        min_y = -max_y;
      }
      if (max_y != 0) {
        plot_ab.destroy();
        plot_ab = $.jqplot('chartdiv_ab', [ab_balance], { seriesDefaults: { showMarker: false, color: '#5cb85c', shadow: false, renderer: $.jqplot.BarRenderer, rendererOptions: { barWidth: 4, fillToZero: true, varyBarColor: false } }, axes: { xaxis: { showTicks: false }, yaxis: { showTicks: false, min: min_y, max: max_y, numberTicks: 3 } }, highlighter: { show: true, sizeAdjust: 5, tooltipAxes: 'y' }, negativeColors: ['#d9534f'] });
      }
    }
    avg_ab = 0; max_y = 0;
  }

  function map_update() {
    if (maps_number <= 0) { return; }
    jQuery.get("/read_json.php?device=map", function (data) {
      var inter = data.split('\n');
      map = JSON.parse(inter[0]);
      if (typeof map == 'undefined') { return; }

      e.map_vin.kumaGauge('update', { value: map['_UNET'] });
      e.map_vout.kumaGauge('update', { value: map['_UOUTmed'] });
      e.map_inet.kumaGauge('update', { value: map['_INET_16_4'] });
      e.map_ubat.kumaGauge('update', { value: map['_Uacc'] });
      e.map_ibat.kumaGauge('update', { value: map['_IAcc_med_A_u16'] });
      e.map_ialt.kumaGauge('update', { value: map['_I_mppt_avg'], max: Math.round(max_mppt_power / voltage[eeprom[0x06]]) });

      e.map_mode_div.html(map_mode[map['_MODE']]);
      e.map_time.html(map['time']);

      //------------------MAP
      var position = 0;
      if (map['_PNET_calc'] != 0) { position = Math.log(Math.abs(map['_PNET_calc'] / 10)) * 47 / Math.log(max_power * 2); }
      if (map['_PNET_calc'] >= 0) { e.map_slider.css('left', (50 + position) + '%'); }
      if (map['_PNET_calc'] < 0) { e.map_slider.css('left', (50 - position) + '%'); }
      var va = "";
      if (!isNaN(map['_PNET_calc'])) { va = Math.round(map['_PNET_calc']) + ' ' + loc['title8'] + 'A</b>'; }
      e.map_slider.html(va);

      if (!isNaN(map['_TFNET'])) { e.f1.html(map['_TFNET'] + loc['hz']); }
      if (!isNaN(map['_ThFMAP'])) { e.f2.html(map['_ThFMAP'] + loc['hz']); }
      if (!isNaN(map['_Temp_Grad2'])) { e.map_temp1.html(map['_Temp_Grad2'] + '&degC'); }
      if (!isNaN(map['_Temp_Grad0'])) { e.map_ab_temp.html(map['_Temp_Grad0'] + '&degC'); }
      if (map_fw >= 25.3 && eeprom[0x13C] === 2) { if (!isNaN(map['_E_NET_SIGN'])) { e.map_enet.html('&nbsp;' + (map['_E_NET_SIGN'] / 100) + ' ' + loc['kWh']); } }
      else if (!isNaN(map['_E_NET'])) { e.map_enet.html('&nbsp;' + (map['_E_NET'] / 100) + ' ' + loc['kWh']); }
      if (!isNaN(map['_E_ACC'])) { e.map_eacc.html('&nbsp;&nbsp;' + (map['_E_ACC'] / 100) + ' ' + loc['kWh']); }
      if (!isNaN(map['_E_ACC_CHARGE'])) { e.map_echg.html('&nbsp;&nbsp;' + (map['_E_ACC_CHARGE'] / 100) + ' ' + loc['kWh']); }

      //-------------ac inputs
      if (map['_flagUnet2'] & 1) {
        e.map_ac_in2.css('transform', 'scale(1,1)');
        e.map_ac_in1.css('transform', 'scale(0.72,0.72)');
      } else {
        e.map_ac_in2.css('transform', 'scale(0.72,0.72)');
        e.map_ac_in1.css('transform', 'scale(1,1)');
      }
      if (map['_flagUnet2'] & 4) { e.map_ac_in2.css('color', '#5cb85c'); } else { e.map_ac_in2.css('color', 'lightgray'); }
      if (map['_flagUnet2'] & 2) { e.map_ac_in1.css('color', '#5cb85c'); } else { e.map_ac_in1.css('color', 'lightgray'); }

      //-----cooler
      if (map['_CoolerSpeed'] > 0) { addClass(e.map_cooler, 'rotating'); } else { removeClass(e.map_cooler, 'rotating'); }

      //---------relays
      if (map['_Relay1'] > 0) { e.map_relay1.css('color', 'white'); e.map_relay1.css('background-color', 'darkgreen'); }
      else { e.map_relay1.css('color', '#8c8c8c'); e.map_relay1.css('background-color', 'grey'); }
      if (map['_Relay2'] > 0) { e.map_relay2.css('color', 'white'); e.map_relay2.css('background-color', 'darkgreen'); }
      else { e.map_relay2.css('color', '#8c8c8c'); e.map_relay2.css('background-color', 'grey'); }

      if (map['_Ubuf_T'] < 100) {
        if (parseInt(map['_fl_UAccChBUF_24h']) > 0) { e.ubuft.html("<b>&darr;b(24" + loc['h'] + ")</b>:&thinsp;" + map['_Ubuf_T']); }
        else { e.ubuft.html(map['_Ubuf_T']); }
      }
      if (map['_Uch_T'] < 100) { e.uchgt.html(map['_Uch_T']); }

      //--------- 3-phases
      if (eeprom[0x139] == 1) {
        var p_calc1 = (Math.round(map['_Uacc'] * map['_I_ph1']) / 1000).toFixed(2);
        var p_calc2 = (Math.round(map['_Uacc'] * map['_I_ph2']) / 1000).toFixed(2);
        var p_calc3 = (Math.round(map['_Uacc'] * map['_I_ph3']) / 1000).toFixed(2);
        var p_all = (Math.round(map['_Uacc'] * map['_I_acc_3ph']) / 1000).toFixed(2);
        if (!isNaN(p_calc1)) { e.ab_ph1.html('&#966;<sub>1</sub>:' + p_calc1 + ' ' + loc['kW']); }
        if (!isNaN(p_calc2)) { e.ab_ph2.html('&#966;<sub>2</sub>:' + p_calc2 + ' ' + loc['kW']); }
        if (!isNaN(p_calc3)) { e.ab_ph3.html('&#966;<sub>3</sub>:' + p_calc2 + ' ' + loc['kW']); }
        if (!isNaN(p_all)) { e.ab_ph_all.html('&#931;&#966;:' + p_all + ' ' + loc['kW']); }
      }

      //======= MAP ERRORS (счётчик; список — позже)
      if ((Number(map['_RSErrSis']) + Number(map['_RSErrJobM']) + Number(map['_RSErrJob']) + Number(map['_RSWarning']) + Number(map['_RSErrDop']) + Number(map['_I2C_Err']) + Number(map['_F_Net_Over']) + Number(map['_F_Acc_Over'])) > 0) {
        removeClass(e.last_error, 'hidden');
      }

      //-------------------AB
      position = 0;
      if (map['_PLoad_calc'] != 0) { position = Math.log(Math.abs(map['_PLoad_calc'] / 10)) * 47 / Math.log(max_power * 2); }
      if (map['_PLoad_calc'] >= 0) { e.ab_slider.css('left', (50 + position) + '%'); }
      if (map['_PLoad_calc'] < 0) { e.ab_slider.css('left', (50 - position) + '%'); }
      if (!isNaN(map['_PLoad_calc'])) { e.ab_slider.html(Math.round(map['_PLoad_calc']) + ' ' + loc['W']); }

      //--------- averages for charts
      if (eeprom[0x139] == 1) { avg_ab = avg_ab + Number(map['_I_acc_3ph'] * map['_Uacc']) + Number(mppt_power_i2c); }
      else { avg_ab = avg_ab + Number(map['_PLoad_calc']) + Number(mppt_power_i2c); }
      if (isNaN(avg_ab)) { avg_ab = 0; }
      avg_map = avg_map + Number(map['_PNET_calc']);
      if (isNaN(avg_map)) { avg_map = 0; }
      if (++counter == 6) { counter = 1; update_charts(); }
    });
  }

  function bind() {
    e.map_ac_in1 = $('#map_input1');
    e.map_ac_in2 = $('#map_input2');
    e.map_inet = $('.map_inet');
    e.map_vin = $('.map_vin');
    e.map_vout = $('.map_vout');
    e.map_ubat = $('.map_uab');
    e.map_ibat = $('.map_ibat');
    e.map_ialt = $('.map_ialt');
    e.map_temp1 = $('#map_temp_1');
    e.map_ab_temp = $('#map_ab_temp');
    e.map_mode_div = $('#map_mode');
    e.map_enet = $('#map_enet');
    e.map_eacc = $('#map_eacc');
    e.map_echg = $('#map_echg');
    e.map_ealt = $('#map_ealt');
    e.map_time = $('#map_time');
    e.map_slider = $('#map_slider');
    e.ab_slider = $('#ab_slider');
    e.mppt_slider = $('#mppt_slider');
    e.f1 = $('#f1');
    e.f2 = $('#f2');
    e.map_cooler = $('#map_cooler');
    e.map_relay1 = $('#map_relay1');
    e.map_relay2 = $('#map_relay2');
    e.ab_ph1 = $('#ab_slider_ph1');
    e.ab_ph2 = $('#ab_slider_ph2');
    e.ab_ph3 = $('#ab_slider_ph3');
    e.ab_ph_all = $('#ab_slider_ph_summ');
    e.last_error = $('#last_error');
    e.errors_badge = $('#errors_count');
    e.ubuft = $('#Ubuf_t');
    e.uchgt = $('#Uchg_t');
    addClass(e.last_error, 'hidden');
  }

  $(document).ready(function () {
    if (maps_number <= 0) { return; }
    $('[data-toggle="tooltip"]').tooltip();

    jQuery.get("/read_memory.php?offset=0&count=400", function (data) {
      eeprom = JSON.parse(data);
      // «ID» в оригинале — серийник МАП (uid = eeprom[0x19]*256 + eeprom[0x18]).
      var uidLabel = document.getElementById('map_id_label');
      if (uidLabel) { uidLabel.textContent = (eeprom[0x18] | (eeprom[0x19] << 8)); }
      bind();
      max_power = power[eeprom[0x05]] * 1500;
      map_fw = parseFloat((eeprom[2] & 0x1F) + (eeprom[2] >> 5) / 10);
      initGauges();
      if (eeprom[0x139] != 1) {
        addClass(e.ab_ph1, 'hidden'); addClass(e.ab_ph2, 'hidden');
        addClass(e.ab_ph3, 'hidden'); addClass(e.ab_ph_all, 'hidden');
      }
      plot_ab = $.jqplot('chartdiv_ab', [ab_balance], { seriesDefaults: { showMarker: false, color: '#428bca', shadow: false, renderer: $.jqplot.BarRenderer, rendererOptions: { barWidth: 4 } }, axes: { xaxis: { showTicks: false }, yaxis: { showTicks: false } }, cursor: { show: true, tooltipLocation: 'sw' } });
      plot_map = $.jqplot('chartdiv_map', [map_balance], { seriesDefaults: { showMarker: false, color: '#428bca', shadow: false, renderer: $.jqplot.BarRenderer, rendererOptions: { barWidth: 4 } }, axes: { xaxis: { showTicks: false }, yaxis: { showTicks: false } }, cursor: { show: true, tooltipLocation: 'sw' } });

      setInterval(map_update, 1000);
      map_update();
    });
  });
})();
