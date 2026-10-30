/**
 * Core utilities — shell helpers, encoding, DOM shortcuts
 */
var Utils = (function () {
    var $ = function (s) { return document.querySelector(s); };
    var $$ = function (s) { return document.querySelectorAll(s); };

    // Escapes the five HTML metacharacters, including both quote characters.
    //
    // The old implementation round-tripped through textContent/innerHTML, which
    // only escapes & < >. Task output and commands are inserted into double
    // quoted attributes (title="…", data-copy-text="…"), so a payload like
    //   ok" onmouseover="alert(1)
    // closed the attribute and installed a real event handler in the Cockpit
    // (root) session. Anything that comes from a task must be escaped for both
    // text and attribute contexts.
    function escHtml(s) {
        if (s === null || s === undefined) return '';
        return String(s)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    function shellQuote(s) {
        return "'" + s.replace(/'/g, "'\\''") + "'";
    }

    // Longest run we accept as a real duration (3 years). Anything above this is
    // not a duration — a wall-clock timestamp used to be stored in this field,
    // which rendered as "1793262922261ms" for a run that failed instantly.
    var MAX_DURATION_MS = 3 * 365 * 24 * 60 * 60 * 1000;

    function pad2(n) {
        return n < 10 ? '0' + n : String(n);
    }

    // Mirrors executor.FormatDuration on the Go side so the CLI and the WebUI
    // describe a run the same way: 850ms, 4.2s, 1m 07s, 2h 05m, 3d 04h.
    // Returns '' when the value cannot be a duration, so callers can hide it
    // instead of printing garbage.
    function formatDuration(ms) {
        if (ms === null || ms === undefined || ms === '') return '';
        var n = Number(ms);
        if (!isFinite(n) || n < 0 || n > MAX_DURATION_MS) return '';
        if (n < 1000) return Math.round(n) + 'ms';
        var sec = n / 1000;
        if (sec < 60) return (sec < 10 ? sec.toFixed(1) : String(Math.round(sec))) + 's';
        var days = Math.floor(n / 86400000);
        var hours = Math.floor(n / 3600000) % 24;
        var minutes = Math.floor(n / 60000) % 60;
        var seconds = Math.floor(n / 1000) % 60;
        if (days > 0) return days + 'd ' + pad2(hours) + 'h';
        if (hours > 0) return hours + 'h ' + pad2(minutes) + 'm';
        return minutes + 'm ' + pad2(seconds) + 's';
    }

    // Formats a Date the same way the Go side stores `created_at`
    // (time.Now().Format("2006-01-02 15:04:05"), local time).
    //
    // This matters because `cronplus prune --before` compares the stored
    // created_at strings lexicographically — so the value has to be exactly
    // this shape, zero padded, local time, no timezone suffix. A JS
    // toISOString() would emit UTC ("2024-05-01T12:00:00.000Z") and either
    // fail to parse or silently shift the cutoff by the UTC offset.
    function formatDateTime(d) {
        var dt = (d instanceof Date) ? d : new Date(d);
        if (isNaN(dt.getTime())) return '';
        return dt.getFullYear() + '-' + pad2(dt.getMonth() + 1) + '-' + pad2(dt.getDate()) +
            ' ' + pad2(dt.getHours()) + ':' + pad2(dt.getMinutes()) + ':' + pad2(dt.getSeconds());
    }

    function b64Encode(str) {
        try { return btoa(unescape(encodeURIComponent(str))); }
        catch (e) { return btoa(str); }
    }

    function b64Decode(str) {
        try { return decodeURIComponent(escape(atob(str))); }
        catch (e) { return null; }
    }

    function shellReadJson(path) {
        var safePath = shellQuote(path);
        return cockpit.spawn(
            ['bash', '-c', 'cat ' + safePath + ' 2>/dev/null || echo "[]"'],
            { err: 'message', environ: ['LC_ALL=C'] }
        ).then(function (out) {
            try { return JSON.parse(out || '[]'); }
            catch (e) { return []; }
        });
    }

    function shellWriteJson(path, data) {
        var json = JSON.stringify(data, null, 2);
        var safePath = shellQuote(path);
        var safeDir = shellQuote(path.replace(/\/[^\/]*$/, ''));
        var script = 'mkdir -p ' + safeDir + ' && printf %s ' + shellQuote(json) + ' | tee ' + safePath + ' > /dev/null && chmod 600 ' + safePath;
        return cockpit.spawn(['bash', '-c', script], { err: 'message', environ: ['LC_ALL=C'] });
    }

    function spawn(cmd, args) {
        return cockpit.spawn([cmd].concat(args || []), { err: 'message', environ: ['LC_ALL=C'] });
    }

    function encodeTaskForSave(task) {
        var copy = {};
        for (var k in task) { if (task.hasOwnProperty(k)) copy[k] = task[k]; }
        if (copy.command) copy.command = b64Encode(copy.command);
        return copy;
    }

    function decodeTaskOnLoad(task) {
        if (task.command) {
            var decoded = b64Decode(task.command);
            if (decoded !== null && decoded !== task.command) {
                task.command = decoded;
            }
        }
        return task;
    }

    return {
        $: $,
        $$: $$,
        escHtml: escHtml,
        shellQuote: shellQuote,
        formatDuration: formatDuration,
        formatDateTime: formatDateTime,
        b64Encode: b64Encode,
        b64Decode: b64Decode,
        shellReadJson: shellReadJson,
        shellWriteJson: shellWriteJson,
        spawn: spawn,
        encodeTaskForSave: encodeTaskForSave,
        decodeTaskOnLoad: decodeTaskOnLoad
    };
})();
