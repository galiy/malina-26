// МАСТЕР-УЗЕЛ — порт legacy js/master_node.js (без изменений логики отрисовки).
// Отличие только в транспорте: вместо POST request_node.php {url,port,dev} —
// GET /api/master/*?id=… (сам remote не дёргаем; адресов узлов не касаемся).
// Убраны мертвые/системные части шапки (wifi/lan/daemons/tick/switch_lang).

var remote_daemons = {},
    map = [],
    map_mppt = [],
    mppt = [],
    batmon = [];
var nodes_number = nodes.length;

var mppt_relay = [0, 1, 2, 4];

function add_Class(element, class_to_add) {
    if (element.hasClass(class_to_add)) return;
    element.addClass(class_to_add);
}

function remove_Class(element, class_to_remove) {
    if (!element.hasClass(class_to_remove)) return;
    element.removeClass(class_to_remove);
}

function get_id(id) { return document.getElementById(id); }

function fail(id) {
    if (typeof id == "undefined") id = 0;
    $('#' + id + '_icon').css('color', 'red');
}

function round(val) {
    var i = Math.round(val * 100);
    return i / 100;
}

// legacy dev -> наш эндпоинт (по id узла).
function masterURL(dev, i) {
    var id = encodeURIComponent(nodes[i]['id']);
    if (dev.indexOf('/check_daemons.php') === 0) return '/api/master/check_daemons?id=' + id;
    var m = /device=([a-zA-Z]+)/.exec(dev);
    return '/api/master/read_json?id=' + id + '&device=' + (m ? m[1] : '');
}

// legacy request_node.php -> наш GET; «__ERROR__» эквивалент ответа "ERROR".
function req(dev, i, cb) {
    jQuery.get(masterURL(dev, i)).done(function (data) { cb(data); })
        .fail(function () { cb('__ERROR__'); });
}

function update_nodes() {
    for (var i = 0; i < nodes_number; i++) {
        req('/check_daemons.php', i, function (data) {
            var n = this.i;
            if (data !== '__ERROR__') { try { remote_daemons[n] = JSON.parse(data); } catch (e) { remote_daemons[n] = 'ERROR'; } }
            else remote_daemons[n] = 'ERROR';
        }.bind({ i: i }));

        if (remote_daemons[i] == 'ERROR' || remote_daemons[i] == false || remote_daemons[i] == null || typeof remote_daemons[i] == 'undefined') {
            fail(i);
        } else $('#' + i + '_icon').css('color', 'lightgreen');

        if (typeof remote_daemons[i] == "undefined") return;

        //------------------------------------------------MAP-------------------------------------------------------------------
        if (remote_daemons[i]["mapd"] == 0) {
            remove_Class($('#' + i + '_map'), 'dimmed');
            remove_Class($('#' + i + '_map_plug'), 'dimmed');
            remove_Class($('#' + i + '_ac_map'), 'dimmed');
            remove_Class($('#' + i + '_dc_map'), 'dimmed');
            remove_Class($('#' + i + '_ac_home'), 'dimmed');
            req('/read_json.php?device=map', i, function (data) {
                var n = this.i;
                if (data !== '__ERROR__') {
                    var inter = data.split('\n');
                    try { map[n] = JSON.parse(inter[0]); } catch (e) { map[n] = 'ERROR'; return; }
                    if (inter[1] && inter[1].length > 0) map_mppt[n] = JSON.parse(inter[1]);
                } else map[n] = 'ERROR';
            }.bind({ i: i }));

            if (map[i] !== 'ERROR' && typeof map[i] !== "undefined") {
                $('#' + i + '_bat_v').html(map[i]['_Uacc'] + '&thinsp;' + loc['V']);
                var _mm = map[i]['_MODE'], _sc = map[i]['_Status_Char'] || 0;
                $('#' + i + '_map_mode').html(_mm === 9 ? ("МАП включен. Трансляция + заряд. Режим " + _sc + " (" + _sc.toString(2) + ")") : (map_mode[_mm] || ""));
                $('#' + i + '_mac_vin').html('~' + map[i]['_UNET'] + '&thinsp;' + loc['V']);
                $('#' + i + '_mac_vout').html('~' + map[i]['_UOUTmed'] + '&thinsp;' + loc['V']);

                if ((map[i]['_RSErrDop'] + map[i]['_I2C_Err'] + map[i]['_RSErrSis'] + map[i]['_RSErrJobM'] + map[i]['_RSErrJob'] + map[i]['_RSWarning']) > 0) $('#' + i + '_info').html($('#' + i + '_info').html() + map[i]['time'] + '&nbsp;' + loc['MAC'] + '&thinsp;' + loc['error'] + '<br>');

                if (map[i]['_Relay1'] > 0) { remove_Class($('#' + i + '_map_relay1'), 'off'); add_Class($('#' + i + '_map_relay1'), 'on'); }
                else { remove_Class($('#' + i + '_map_relay1'), 'on'); add_Class($('#' + i + '_map_relay1'), 'off'); }
                if (map[i]['_Relay2'] > 0) { remove_Class($('#' + i + '_map_relay2'), 'off'); add_Class($('#' + i + '_map_relay2'), 'on'); }
                else { remove_Class($('#' + i + '_map_relay2'), 'on'); add_Class($('#' + i + '_map_relay2'), 'off'); }

                if ((map[i]['_flagUnet2'] & 1) > 0) $('#' + i + '_map_input').html("&#10103;");
                else $('#' + i + '_map_input').html("&#10102;");

                if (map[i]['_INET_16_4'] == 0) { remove_Class($('#' + i + '_ac_map'), 'active'); $('#' + i + '_mac_iac').empty(); }
                else add_Class($('#' + i + '_ac_map'), 'active');
                if (map[i]['_INET_16_4'] < 0) {
                    remove_Class($('#' + i + '_ac_map'), 'reverse');
                    $('#' + i + '_mac_iac').html("<span class='glyphicons glyphicons-arrow-left drop'></span>&thinsp;" + map[i]['_INET_16_4'] + '&thinsp;' + 'A&nbsp;' + (round(Math.round(map[i]['_PNET_calc']) / 1000)) + '&thinsp;' + loc['kVA']);
                } else if (map[i]['_INET_16_4'] > 0) {
                    add_Class($('#' + i + '_ac_map'), 'reverse');
                    $('#' + i + '_mac_iac').html("<span class='glyphicons glyphicons-arrow-right drop'></span>&thinsp;" + map[i]['_INET_16_4'] + '&thinsp;' + 'A&nbsp;' + (round(Math.round(map[i]['_PNET_calc']) / 1000)) + '&thinsp;' + loc['kVA']);
                }

                if (map[i]['_IAcc_med_A_u16'] == 0) { remove_Class($('#' + i + '_dc_map'), 'active'); $('#' + i + '_mac_dc').empty(); }
                else add_Class($('#' + i + '_dc_map'), 'active');
                if (map[i]['_IAcc_med_A_u16'] < 0) {
                    add_Class($('#' + i + '_dc_map'), 'reverse');
                    $('#' + i + '_mac_dc').html("<span class='glyphicons glyphicons-arrow-up drop'></span>&thinsp;" + map[i]['_IAcc_med_A_u16'] + '&thinsp;' + 'A,&nbsp;' + (round(Math.round(map[i]['_PLoad_calc']) / 1000)) + '&thinsp;' + loc['kW']);
                } else if (map[i]['_IAcc_med_A_u16'] > 0) {
                    remove_Class($('#' + i + '_dc_map'), 'reverse');
                    $('#' + i + '_mac_dc').html("<span class='glyphicons glyphicons-arrow-down drop'></span>&thinsp;" + map[i]['_IAcc_med_A_u16'] + '&thinsp;' + 'A,&nbsp;' + (round(Math.round(map[i]['_PLoad_calc']) / 1000)) + '&thinsp;' + loc['kW']);
                }

                if (map[i]['_UNET'] > 100) $('#' + i + '_map_plug').css('color', 'darkblue');
                else $('#' + i + '_map_plug').css('color', '#dc6460');

                if (map[i]['_MODE'] > 1) add_Class($('#' + i + '_ac_home'), 'active');
                else remove_Class($('#' + i + '_ac_home'), 'active');

                $('#' + i + '_bat_t').html("<span class='glyphicons glyphicons-temperature drop'></span>&thinsp;" + map[i]['_Temp_Grad0'] + '&thinsp;' + '&deg;C');
                $('#' + i + '_map_t').html("<span class='glyphicons glyphicons-temperature drop'></span>&thinsp;" + map[i]['_Temp_Grad2'] + '&thinsp;' + '&deg;C');
            }
        } else {
            add_Class($('#' + i + '_map'), 'dimmed');
            add_Class($('#' + i + '_map_plug'), 'dimmed');
            add_Class($('#' + i + '_ac_map'), 'dimmed');
            add_Class($('#' + i + '_dc_map'), 'dimmed');
            add_Class($('#' + i + '_ac_home'), 'dimmed');
        }

        //------------------------------------------BATMON-------------------------------------------------------------------
        if (remote_daemons[i]["batmond"] == 0) {
            req('/read_json.php?device=bat', i, function (data) {
                var n = this.i;
                if (data !== '__ERROR__') { try { batmon[n] = JSON.parse(data); } catch (e) { batmon[n] = 'ERROR'; } }
                else batmon[n] = 'ERROR';
            }.bind({ i: i }));

            if (batmon[i] !== 'ERROR' && batmon[i] !== null && (typeof batmon[i] !== "undefined")) {
                $('#' + i + '_bat_100').html(batmon[i][0]['C_100_remain'] + '&thinsp;' + '%');
                $('#' + i + '_map_bat_h').html(batmon[i][0]['C_100_remain'] + '&thinsp;' + '%');
                $('#' + i + '_alt_e').html((batmon[i][0]['mppt_day_E'] / 1000) + '&thinsp;' + loc['kWh']);
            }
        }

        //-------------------------------------CHARGER------------------------------------------------------------------------
        if (remote_daemons[i]["chargerd"] == 0) {
            remove_Class($('#' + i + '_charger'), 'dimmed');
            remove_Class($('#' + i + '_dc_charger'), 'dimmed');
            remove_Class($('#' + i + '_charger_plug'), 'dimmed');
        } else {
            add_Class($('#' + i + '_charger'), 'dimmed');
            add_Class($('#' + i + '_charger_plug'), 'dimmed');
            add_Class($('#' + i + '_dc_charger'), 'dimmed');
        }

        //-----------------------------------------MPPTS------------------------------------------------------------------------
        if ((remote_daemons[i]["mpptd0"] + remote_daemons[i]["mpptd1"] + remote_daemons[i]["mpptd2"] + remote_daemons[i]["mpptd3"]) < 4) {
            for (var ind = 1; ind <= 4; ind++)
                if (remote_daemons[i]["mpptd" + (ind - 1)] == 0) remove_Class($('#' + i + '_mppt' + ind), 'dimmed');
                else add_Class($('#' + i + '_mppt' + ind), 'dimmed');

            remove_Class($('#' + i + '_dc_mppt'), 'dimmed');

            req('/read_json.php?device=mppt', i, function (data) {
                var n = this.i;
                if (data !== '__ERROR__') { try { mppt[n] = JSON.parse(data); } catch (e) { mppt[n] = 'ERROR'; } }
                else mppt[n] = 'ERROR';
            }.bind({ i: i }));

            if (mppt[i] !== 'ERROR' && mppt[i] !== null && (typeof mppt[i] !== "undefined")) {
                if (map[i] == null || typeof map[i] == "undefined") $('#' + i + '_bat_v').html(mppt[i][0]['V_Bat'] + '&thinsp;' + loc['V']);

                var P_summ = 0, I_summ = 0, E_summ = 0;
                for (var ind = 0; ind < mppt[i].length; ind++) {
                    $('#' + i + '_mppt' + (ind + 1) + '_vpv').html(Math.round(mppt[i][ind]['Vc_PV']) + '&thinsp;' + loc['V']);
                    $('#' + i + '_mppt' + (ind + 1) + '_pout').html(Math.round(mppt[i][ind]['P_Out']) + '&thinsp;' + loc['W']);
                    $('#' + i + '_mppt' + (ind + 1) + '_t').html(Math.round(mppt[i][ind]['Temp_Int']) + '&deg;C');

                    I_summ += mppt[i][ind]['I_Ch'];
                    P_summ += mppt[i][ind]['P_Out'];
                    E_summ += mppt[i][ind]['Pwr_kW'];

                    for (var relay = 1; relay <= 3; relay++)
                        if ((mppt[i][ind]['Relay_C'] & mppt_relay[relay]) > 0) {
                            remove_Class($('#' + i + '_mppt' + (ind + 1) + '_relay' + relay), 'off');
                            add_Class($('#' + i + '_mppt' + (ind + 1) + '_relay' + relay), 'on');
                        } else {
                            remove_Class($('#' + i + '_mppt' + (ind + 1) + '_relay' + relay), 'on');
                            add_Class($('#' + i + '_mppt' + (ind + 1) + '_relay' + relay), 'off');
                        }

                    if (mppt[i][ind]['RSErrSis'] > 0) $('#' + i + '_info').html($('#' + i + '_info').html() + mppt[i][ind]['time'] + '&nbsp;' + loc['MPPT'] + (ind + 1) + '&thinsp;' + loc['error'] + '<br>');
                }
                if (remote_daemons[i]["batmond"] !== 0) $('#' + i + '_alt_e').html((E_summ / 1000) + '&thinsp;' + loc['kWh']);
            }
        } else if (map_mppt[i] !== null && map_mppt[i] !== 'ERROR' && (typeof map_mppt[i] !== "undefined")) {
            for (var ind = 2; ind <= 4; ind++) add_Class($('#' + i + '_mppt' + ind), 'dimmed');
            $('#' + i + '_mppt1_vpv').html("I2C");
            $('#' + i + '_mppt1_pout').html(map_mppt[i].length + '&thinsp;MPPTs');
        } else {
            for (var ind = 1; ind <= 4; ind++) add_Class($('#' + i + '_mppt' + ind), 'dimmed');
            add_Class($('#' + i + '_dc_mppt'), 'dimmed');
        }

        //------------------------ MPPTs current & power
        if (map_mppt[i] !== null && map_mppt[i] !== 'ERROR' && (typeof map_mppt[i] !== "undefined")) {
            if (map[i]['_I_mppt_avg'] == 0) {
                remove_Class($('#' + i + '_dc_mppt'), 'active');
                $('#' + i + '_mppts_idc').empty();
                $('#' + i + '_mppts_pdc').empty();
            } else if (map[i]['_I_mppt_avg'] > 0) {
                $('#' + i + '_mppts_idc').html(map[i]['_I_mppt_avg'] + '&thinsp;A');
                var power = Math.round((map[i]['_I_mppt_avg'] * map[i]['_Uacc']) / 10) / 100;
                $('#' + i + '_mppts_pdc').html(power + '&thinsp;' + loc['kW']);
                add_Class($('#' + i + '_dc_mppt'), 'active');
            }
        } else if (I_summ == 0) {
            remove_Class($('#' + i + '_dc_mppt'), 'active');
            $('#' + i + '_mppts_idc').empty();
            $('#' + i + '_mppts_pdc').empty();
        } else if (I_summ > 0) {
            $('#' + i + '_mppts_idc').html(map[i]['_I_mppt_avg'] + '&thinsp;A');
            var power = Math.round(P_summ / 10) / 100;
            $('#' + i + '_mppts_pdc').html(power + '&thinsp;' + loc['kW']);
            add_Class($('#' + i + '_dc_mppt'), 'active');
        }
    }
}

$(document).ready(function () {
    setInterval(function () { update_nodes(); }, 3000);
    update_nodes();
});
