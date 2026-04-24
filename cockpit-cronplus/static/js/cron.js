/**
 * Cron expression parsing, preview, description, and next-run calculation
 * 
 * BUG FIXES applied:
 * 1. DOW matching: treat 0 and 7 both as Sunday (cron standard)
 * 2. DOW description: show "周日" for DOW=7 (was showing "周7")
 * 3. getNextRunTimes: rewritten with field-skipping algorithm (was O(seconds), now O(fields))
 *    - Old: iterated second-by-second, could not calculate schedules >115 days away
 *    - New: jumps to next valid value per field, handles any schedule instantly
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
        // BUG FIX: Added index 7 = '日' (Sunday)
        var dn = ['日', '一', '二', '三', '四', '五', '六', '日'];
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

    // Parse a field spec into a sorted array of valid values
    function parseFieldValues(spec, min, max) {
        if (spec === '*') return null; // null = wildcard
        var valSet = {};
        var parts = spec.split(',');
        parts.forEach(function (part) {
            part = part.trim();
            var sm = part.match(/^(.*?)\/(\d+)$/);
            if (sm) {
                var step = parseInt(sm[2]);
                if (sm[1] === '*') {
                    for (var v = min; v <= max; v += step) valSet[v] = true;
                } else if (sm[1].indexOf('-') >= 0) {
                    var bp = sm[1].split('-');
                    for (var v = parseInt(bp[0]); v <= parseInt(bp[1]); v += step) valSet[v] = true;
                } else {
                    valSet[parseInt(sm[1])] = true;
                }
                return;
            }
            var rm = part.match(/^(\d+)-(\d+)$/);
            if (rm) {
                for (var v = parseInt(rm[1]); v <= parseInt(rm[2]); v++) valSet[v] = true;
                return;
            }
            var n = parseInt(part);
            if (!isNaN(n)) valSet[n] = true;
        });
        return Object.keys(valSet).map(Number).sort(function (a, b) { return a - b; });
    }

    // Find the smallest value > current, or wrap to first value
    function fieldNextAfter(values, current) {
        if (values === null) return current + 1; // wildcard: just increment
        for (var i = 0; i < values.length; i++) {
            if (values[i] > current) return values[i];
        }
        return values[0]; // wrap
    }

    function fieldMin(values, fallback) {
        if (values === null) return fallback;
        return values.length > 0 ? values[0] : fallback;
    }

    function fieldMatches(values, val) {
        if (values === null) return true;
        // Binary search
        var lo = 0, hi = values.length - 1;
        while (lo <= hi) {
            var mid = (lo + hi) >> 1;
            if (values[mid] === val) return true;
            if (values[mid] < val) lo = mid + 1; else hi = mid - 1;
        }
        return false;
    }

    // ============================================================
    // Smart next-run calculation using field-level skipping
    // Equivalent to Go backend's NextRunTime function
    // ============================================================
    function nextRunTimeAfter(fields, after) {
        var secV  = parseFieldValues(fields[0], 0, 59);
        var minV  = parseFieldValues(fields[1], 0, 59);
        var hourV = parseFieldValues(fields[2], 0, 23);
        var dayV  = parseFieldValues(fields[3], 1, 31);
        var monthV = parseFieldValues(fields[4], 1, 12);
        var dowV  = parseFieldValues(fields[5], 0, 7);

        var cur = new Date(after.getTime() + 1000);
        cur.setMilliseconds(0);
        var maxCheck = new Date(cur.getTime() + 4 * 365 * 24 * 3600 * 1000); // 4 years

        var iterations = 0;
        while (cur < maxCheck && iterations < 100000) {
            iterations++;

            // 1. Month
            if (monthV !== null && !fieldMatches(monthV, cur.getMonth() + 1)) {
                var nm = fieldNextAfter(monthV, cur.getMonth() + 1);
                if (nm <= cur.getMonth() + 1) {
                    cur = new Date(cur.getFullYear() + 1, fieldMin(monthV, 1) - 1, 1, 0, 0, 0);
                } else {
                    cur = new Date(cur.getFullYear(), nm - 1, 1, 0, 0, 0);
                }
                continue;
            }

            // 2. Day + DOW (OR logic when both specified)
            var dayMatch = dayV === null ? true : fieldMatches(dayV, cur.getDate());
            var dowVal = cur.getDay(); // 0=Sunday
            var dowMatch = dowV === null ? true : fieldMatches(dowV, dowVal);
            // BUG FIX: Sunday = 0 or 7
            if (!dowMatch && dowVal === 0 && dowV !== null) {
                dowMatch = fieldMatches(dowV, 7);
            }

            var daySpec = fields[3];
            var dowSpec = fields[5];
            if (daySpec !== '*' && dowSpec !== '*') {
                if (!dayMatch && !dowMatch) {
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate() + 1, 0, 0, 0);
                    continue;
                }
            } else {
                if (daySpec !== '*' && !dayMatch) {
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate() + 1, 0, 0, 0);
                    continue;
                }
                if (dowSpec !== '*' && !dowMatch) {
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate() + 1, 0, 0, 0);
                    continue;
                }
            }

            // 3. Hour
            if (hourV !== null && !fieldMatches(hourV, cur.getHours())) {
                var nh = fieldNextAfter(hourV, cur.getHours());
                if (nh <= cur.getHours()) {
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate() + 1, fieldMin(hourV, 0), 0, 0);
                } else {
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate(), nh, 0, 0);
                }
                continue;
            }

            // 4. Minute
            if (minV !== null && !fieldMatches(minV, cur.getMinutes())) {
                var nmin = fieldNextAfter(minV, cur.getMinutes());
                if (nmin <= cur.getMinutes()) {
                    // Wrap to next hour
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate(), cur.getHours() + 1, fieldMin(minV, 0), 0);
                } else {
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate(), cur.getHours(), nmin, 0);
                }
                continue;
            }

            // 5. Second
            if (secV !== null && !fieldMatches(secV, cur.getSeconds())) {
                var ns = fieldNextAfter(secV, cur.getSeconds());
                if (ns <= cur.getSeconds()) {
                    // Wrap to next minute
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate(), cur.getHours(), cur.getMinutes() + 1, fieldMin(secV, 0));
                } else {
                    cur = new Date(cur.getFullYear(), cur.getMonth(), cur.getDate(), cur.getHours(), cur.getMinutes(), ns);
                }
                continue;
            }

            // All fields match!
            return cur;
        }

        return null; // no match found
    }

    function matchCron6(date, sec, min, hour, day, month, dow) {
        if (!matchField(date.getSeconds(), sec, 0, 59)) return false;
        if (!matchField(date.getMinutes(), min, 0, 59)) return false;
        if (!matchField(date.getHours(), hour, 0, 23)) return false;
        if (!matchField(date.getMonth() + 1, month, 1, 12)) return false;
        var dayMatch = matchField(date.getDate(), day, 1, 31);
        var dowVal = date.getDay();
        var dowMatch = matchField(dowVal, dow, 0, 7);
        if (!dowMatch && dowVal === 0 && dow !== '*') {
            dowMatch = matchField(7, dow, 0, 7);
        }
        if (day !== '*' && dow !== '*') { if (!dayMatch && !dowMatch) return false; }
        else { if (day !== '*' && !dayMatch) return false; if (dow !== '*' && !dowMatch) return false; }
        return true;
    }

    function getNextRunTimes(sec, min, hour, day, month, dow) {
        var fields = [sec || '0', min || '*', hour || '*', day || '*', month || '*', dow || '*'];
        var results = [];
        var now = new Date();
        var cur = now;
        for (var i = 0; i < 5; i++) {
            var nxt = nextRunTimeAfter(fields, cur);
            if (!nxt) break;
            results.push(nxt);
            cur = nxt;
        }
        return results;
    }

    function getNextRunTime(sec, min, hour, day, month, dow) {
        var runs = getNextRunTimes(sec, min, hour, day, month, dow);
        return runs[0] || null;
    }

    // Detect "*/N" step pattern and return the step value (or 0 if not a step pattern)
    function _getStep(spec) {
        if (!spec || spec === '*') return 0;
        var m = spec.match(/^\*\/(\d+)$/);
        if (m) return parseInt(m[1]);
        return 0;
    }

    /**
     * Calculate next run times using a custom base datetime for "every N" intervals.
     * For fields with step (slash) pattern, the interval is calculated from baseTime instead of epoch.
     * Other fields (fixed values, ranges, etc.) are respected as-is.
     *
     * @param {string} sec - seconds field
     * @param {string} min - minutes field
     * @param {string} hour - hours field
     * @param {string} day - days field
     * @param {string} month - months field
     * @param {string} dow - day-of-week field
     * @param {Date} baseTime - reference datetime for interval calculation
     * @param {number} count - number of results to return (default 5)
     * @returns {Date[]} array of next run times
     */
    function getNextRunTimesFromBase(sec, min, hour, day, month, dow, baseTime, count) {
        count = count || 5;
        var fields = [sec || '0', min || '*', hour || '*', day || '*', month || '*', dow || '*'];
        var now = new Date();

        // Detect which fields have step patterns
        var secStep = _getStep(sec);
        var minStep = _getStep(min);
        var hourStep = _getStep(hour);
        var dayStep = _getStep(day);

        // Determine the primary interval field (largest time unit with a step)
        // Priority: day > hour > minute > second
        var intervalMs = 0;
        var anchorField = '';
        if (dayStep > 0) {
            intervalMs = dayStep * 86400000;
            anchorField = 'day';
        } else if (hourStep > 0) {
            intervalMs = hourStep * 3600000;
            anchorField = 'hour';
        } else if (minStep > 0) {
            intervalMs = minStep * 60000;
            anchorField = 'min';
        } else if (secStep > 0) {
            intervalMs = secStep * 1000;
            anchorField = 'sec';
        }

        // If no step pattern found, fall back to standard calculation
        if (intervalMs === 0) {
            return getNextRunTimes(sec, min, hour, day, month, dow);
        }

        var base = baseTime instanceof Date ? baseTime : new Date(baseTime);
        if (isNaN(base.getTime())) {
            return getNextRunTimes(sec, min, hour, day, month, dow);
        }

        // Calculate how many intervals have elapsed from base to now
        var elapsed = now.getTime() - base.getTime();
        var intervalsPassed = Math.floor(elapsed / intervalMs);

        // Start from the next interval after now
        var startTime = base.getTime() + (intervalsPassed + 1) * intervalMs;

        // If base is in the future, start from base itself
        if (base.getTime() > now.getTime()) {
            startTime = base.getTime();
        }

        var results = [];
        for (var i = 0; i < count; i++) {
            var t = new Date(startTime + i * intervalMs);

            // For day intervals, we need to respect the time-of-day from base
            if (anchorField === 'day') {
                t = new Date(startTime + i * intervalMs);
            }

            // Verify this time matches all non-step fields using standard cron matching
            // For step fields, we trust our interval calculation
            if (matchCron6(t, sec, min, hour, day, month, dow)) {
                results.push(t);
            } else {
                // If it doesn't match (e.g., dow constraint), use nextRunTimeAfter to find the next valid time
                var prev = i === 0 ? new Date(startTime - 1000) : results[results.length - 1];
                var fallback = nextRunTimeAfter(fields, prev);
                if (fallback && results.length < count) {
                    // Avoid duplicates
                    if (!results.length || fallback.getTime() !== results[results.length - 1].getTime()) {
                        results.push(fallback);
                    }
                }
                // Recalculate next from this point
                for (var j = results.length; j < count; j++) {
                    var nxt = nextRunTimeAfter(fields, results[results.length - 1]);
                    if (!nxt) break;
                    results.push(nxt);
                }
                break;
            }
        }

        // If we didn't get enough results from interval calculation, fill with standard method
        if (results.length < count) {
            var lastRef = results.length > 0 ? results[results.length - 1] : now;
            for (var k = results.length; k < count; k++) {
                var nxt2 = nextRunTimeAfter(fields, lastRef);
                if (!nxt2) break;
                results.push(nxt2);
                lastRef = nxt2;
            }
        }

        return results.slice(0, count);
    }

    return {
        parseSchedule: parseSchedule,
        describeSpecial: describeSpecial,
        describeCron: describeCron,
        updatePreview: updatePreview,
        getNextRunTimes: getNextRunTimes,
        getNextRunTime: getNextRunTime,
        getNextRunTimesFromBase: getNextRunTimesFromBase
    };
})();
