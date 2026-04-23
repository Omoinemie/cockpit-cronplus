/**
 * Cron expression parsing, preview, description, and next-run calculation
 */
var CronUtil = (function () {

    function parseSchedule(schedule) {
        if (!schedule) return { sec: '0', min: '*', hour: '*', day: '*', month: '*', dow: '*' };
        var parts = schedule.split(/\s+/);
        if (parts.length === 6) return { sec: parts[0], min: parts[1], hour: parts[2], day: parts[3], month: parts[4], dow: parts[5] };
        if (parts.length === 5) return { sec: '0', min: parts[0], hour: parts[1], day: parts[2], month: parts[3], dow: parts[4] };
        return { sec: '0', min: '*', hour: '*', day: '*', month: '*', dow: '*' };
    }

    function describeSpecial(s) {
        var map = {
            '@reboot': I18n.t('schedule.reboot')
        };
        return map[s] || s;
    }

    function describeCron(sec, min, hour, day, month, dow) {
        var parts = [];
        if (sec !== '0' && sec !== '*') {
            if (sec.indexOf('/') >= 0) parts.push('每' + sec.split('/')[1] + '秒');
            else parts.push('第' + sec + '秒');
        }
        if (min === '*') parts.push('每分钟');
        else if (min.indexOf('/') >= 0) parts.push('每' + min.split('/')[1] + '分钟');
        else parts.push('第' + min + '分钟');
        if (hour !== '*') {
            if (hour.indexOf('/') >= 0) parts.push('每' + hour.split('/')[1] + '小时');
            else parts.push('第' + hour + '时');
        }
        if (day !== '*') parts.push(day + '日');
        if (month !== '*') parts.push(month + '月');
        var dn = ['日', '一', '二', '三', '四', '五', '六'];
        if (dow !== '*') {
            if (dow.indexOf(',') >= 0) {
                parts.push('周' + dow.split(',').map(function (d) { return dn[parseInt(d)] || d; }).join('、'));
            } else {
                parts.push('周' + (dn[parseInt(dow)] || dow));
            }
        }
        return I18n.t('schedule.execute') + ': ' + parts.join(' · ');
    }

    function updatePreview() {
        var $ = Utils.$;
        var sec = $('#cronSec').value.trim();
        var min = $('#cronMin').value.trim();
        var hour = $('#cronHour').value.trim();
        var day = $('#cronDay').value.trim();
        var month = $('#cronMonth').value.trim();
        var dow = $('#cronDow').value.trim();
        var preview, desc;
        if (min === '@reboot') {
            preview = '@reboot';
            desc = describeSpecial('@reboot');
        } else {
            preview = (sec || '0') + ' ' + (min || '*') + ' ' + (hour || '*') + ' ' + (day || '*') + ' ' + (month || '*') + ' ' + (dow || '*');
            desc = describeCron(sec, min, hour, day, month, dow);
        }
        $('#cronPreview').textContent = preview;
        $('#cronDescription').textContent = desc;
    }

    function matchField(value, spec, min, max) {
        if (spec === '*') return true;
        if (spec.indexOf(',') >= 0) return spec.split(',').some(function (s) { return matchField(value, s.trim(), min, max); });
        var rm = spec.match(/^(\d+)-(\d+)$/);
        if (rm) return value >= parseInt(rm[1]) && value <= parseInt(rm[2]);
        var sm = spec.match(/^(.*?)\/(\d+)$/);
        if (sm) {
            var base = sm[1], step = parseInt(sm[2]);
            if (base === '*') return (value - min) % step === 0;
            if (base.indexOf('-') >= 0) {
                var bp = base.split('-');
                return value >= parseInt(bp[0]) && value <= parseInt(bp[1]) && (value - parseInt(bp[0])) % step === 0;
            }
            return value === parseInt(base);
        }
        return value === parseInt(spec);
    }

    function matchCron6(date, sec, min, hour, day, month, dow) {
        if (!matchField(date.getSeconds(), sec, 0, 59)) return false;
        if (!matchField(date.getMinutes(), min, 0, 59)) return false;
        if (!matchField(date.getHours(), hour, 0, 23)) return false;
        if (!matchField(date.getMonth() + 1, month, 1, 12)) return false;
        var dayMatch = matchField(date.getDate(), day, 1, 31);
        var dowMatch = matchField(date.getDay(), dow, 0, 7);
        if (day !== '*' && dow !== '*') { if (!dayMatch && !dowMatch) return false; }
        else { if (day !== '*' && !dayMatch) return false; if (dow !== '*' && !dowMatch) return false; }
        return true;
    }

    function getNextRunTimes(sec, min, hour, day, month, dow) {
        var results = [];
        var now = new Date();
        var current = new Date(now.getFullYear(), now.getMonth(), now.getDate(),
            now.getHours(), now.getMinutes(), now.getSeconds() + 1, 0);
        var maxIter = current.getTime() + 2 * 365.25 * 24 * 3600 * 1000;
        var safety = 0;
        while (results.length < 5 && current.getTime() < maxIter && safety < 500000) {
            safety++;
            if (matchCron6(current, sec, min, hour, day, month, dow)) results.push(new Date(current));
            current = new Date(current.getTime() + 1000);
        }
        return results;
    }

    function getNextRunTime(sec, min, hour, day, month, dow) {
        var runs = getNextRunTimes(sec, min, hour, day, month, dow);
        return runs[0] || null;
    }

    return {
        parseSchedule: parseSchedule,
        describeSpecial: describeSpecial,
        describeCron: describeCron,
        updatePreview: updatePreview,
        getNextRunTimes: getNextRunTimes,
        getNextRunTime: getNextRunTime
    };
})();
