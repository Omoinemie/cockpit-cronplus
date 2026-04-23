/**
 * Core utilities — shell helpers, encoding, DOM shortcuts
 */
var Utils = (function () {
    var $ = function (s) { return document.querySelector(s); };
    var $$ = function (s) { return document.querySelectorAll(s); };

    function escHtml(s) {
        var d = document.createElement('div');
        d.textContent = s;
        return d.innerHTML;
    }

    function shellQuote(s) {
        return "'" + s.replace(/'/g, "'\\''") + "'";
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
        return cockpit.spawn(
            ['bash', '-c', 'cat ' + path + ' 2>/dev/null || echo "[]"'],
            { err: 'message', environ: ['LC_ALL=C'] }
        ).then(function (out) {
            try { return JSON.parse(out || '[]'); }
            catch (e) { return []; }
        });
    }

    function shellWriteJson(path, data) {
        var json = JSON.stringify(data, null, 2);
        var script = 'mkdir -p $(dirname ' + path + ') && printf %s ' + shellQuote(json) + ' | tee ' + path + ' > /dev/null';
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
        b64Encode: b64Encode,
        b64Decode: b64Decode,
        shellReadJson: shellReadJson,
        shellWriteJson: shellWriteJson,
        spawn: spawn,
        encodeTaskForSave: encodeTaskForSave,
        decodeTaskOnLoad: decodeTaskOnLoad
    };
})();
