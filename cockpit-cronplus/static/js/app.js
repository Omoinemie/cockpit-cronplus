/**
 * Cockpit Cronplus v1.1.8 — Main Application
 * Direct file management via cockpit.spawn — no HTTP API.
 * Config: /opt/cronplus/tasks.conf (JSON array)
 * Logs:   /opt/cronplus/logs/logs.json (JSON array)
 */
(function () {
    'use strict';

    if (typeof cockpit === 'undefined') {
        document.body.innerHTML = '<div style="color:#f87171;padding:40px;text-align:center;font-family:sans-serif">' +
            '<h2 id="errTitle"></h2><p id="errHint"></p></div>';
        return;
    }

    // ===== Paths =====
    var CONF_FILE = '/opt/cronplus/tasks.conf';
    var LOGS_FILE = '/opt/cronplus/logs';
    var SETTINGS_FILE = '/opt/cronplus/settings.json';
    var DAEMON_LOG_FILE = '/opt/cronplus/logs/cronplus.log';

    // ===== State =====
    var tasks = [];
    var logs = [];
    var editingIndex = -1;
    var daemonLogTimer = null;
    var daemonLogOffset = 0;    // byte offset for incremental tail
    var daemonLogLines = [];    // parsed log lines buffer
    var DAEMON_LOG_MAX = 500;   // max lines in memory (default, overridden by setting)
    var systemUsers = [];
    var refreshing = false;
    var autoRefreshTimer = null;
    var AUTO_REFRESH_MS = 15000;
    var userInteracting = false;
    var interactionTimer = null;
    var logPage = 1;
    var LOG_PAGE_SIZE = 20;
    var appSettings = {};  // loaded from settings.json
    var runningProc = null;  // currently running cockpit.spawn process for task execution
    var runStartTime = 0;    // timestamp when current run started
    var runDurationTimer = null; // duration update timer
    var runTimeoutTimer = null;  // timeout timer
    var pendingRunTask = null;   // task object waiting in the output modal
    var currentRunID = null;     // run ID for current execution

    var $ = Utils.$;
    var $$ = Utils.$$;

    // ===== Init =====
    document.addEventListener('DOMContentLoaded', function () {
        // Load i18n first, then init app
        var lang = I18n.detectLang();
        I18n.load(lang).then(function () {
            init();
        });
    });

    async function init() {
        I18n.applyToDOM();
        bindEvents();
        await loadSettings();       // Load settings first (affects defaults)
        initLangSwitcher();
        Theme.init();
        await loadSystemUsers();
        await loadUser();
        await loadTasks();
        await loadLogs();
        renderTasks();
        renderLogs();
        startAutoRefresh();
        initFooter();
    }

    // ===== Settings =====
    var SETTINGS_DEFAULTS = {
        language: 'en',
        theme: 'auto',
        autoRefreshInterval: 15,
        logMaxBytes: 10485760,
        logBackupCount: 5,
        defaultRunUser: 'root',
        defaultTimeout: 0,
        defaultMaxRetries: 0,
        defaultRetryInterval: 60,
        logPageSize: 20,
        taskPageSize: 20,
        daemonLogLevel: 'all',
        daemonLogLines: 100,
        daemonLogInterval: 2,
        daemonLogMaxBytes: 10485760,
        daemonLogBackupCount: 3
    };

    async function loadSettings() {
        try {
            var raw = await Utils.shellReadJson(SETTINGS_FILE);
            appSettings = Object.assign({}, SETTINGS_DEFAULTS, raw || {});
        } catch (e) {
            appSettings = Object.assign({}, SETTINGS_DEFAULTS);
        }
        applySettings();
    }

    function applySettings() {
        // Apply auto-refresh interval
        AUTO_REFRESH_MS = (appSettings.autoRefreshInterval || 15) * 1000;
        if (autoRefreshTimer) {
            stopAutoRefresh();
            startAutoRefresh();
        }
        // Apply log page size
        LOG_PAGE_SIZE = appSettings.logPageSize || 20;
        // Apply theme (if not auto, override)
        if (appSettings.theme && appSettings.theme !== 'auto') {
            Theme.apply(appSettings.theme);
        }
        // Apply daemon log prefs to controls if they exist
        var dll = $('#daemonLogLevel');
        if (dll) dll.value = appSettings.daemonLogLevel || 'all';
        var dln = $('#daemonLogLines');
        if (dln) dln.value = appSettings.daemonLogLines || 100;
        var dli = $('#daemonLogInterval');
        if (dli) dli.value = appSettings.daemonLogInterval || 2;
    }

    async function saveSettings(settings) {
        await Utils.shellWriteJson(SETTINGS_FILE, settings);
        appSettings = Object.assign({}, SETTINGS_DEFAULTS, settings);
        applySettings();
    }

    function openSettingsModal() {
        var s = appSettings;
        $('#setLanguage').value = s.language || 'en';
        $('#setTheme').value = s.theme || 'auto';
        $('#setDefaultUser').value = s.defaultRunUser || 'root';
        $('#setDefaultTimeout').value = s.defaultTimeout || 0;
        $('#setDefaultRetries').value = s.defaultMaxRetries || 0;
        $('#setDefaultRetryInterval').value = s.defaultRetryInterval || 60;
        $('#setAutoRefresh').value = s.autoRefreshInterval || 15;
        $('#setLogPageSize').value = s.logPageSize || 20;
        $('#setLogMaxBytes').value = s.logMaxBytes || 10485760;
        $('#setLogBackupCount').value = s.logBackupCount || 5;
        $('#setDaemonLogLevel').value = s.daemonLogLevel || 'all';
        $('#setDaemonLogLines').value = s.daemonLogLines || 100;
        $('#setDaemonLogInterval').value = s.daemonLogInterval || 2;
        $('#setDaemonLogMaxBytes').value = s.daemonLogMaxBytes || 10485760;
        $('#setDaemonLogBackupCount').value = s.daemonLogBackupCount || 3;

        // Populate user dropdown
        var userSelect = $('#setDefaultUser');
        userSelect.innerHTML = '<option value="root">root</option>';
        systemUsers.forEach(function (u) {
            if (u !== 'root') {
                userSelect.innerHTML += '<option value="' + Utils.escHtml(u) + '">' + Utils.escHtml(u) + '</option>';
            }
        });
        userSelect.value = s.defaultRunUser || 'root';

        $('#settingsModal').style.display = '';
    }

    function closeSettingsModal() {
        $('#settingsModal').style.display = 'none';
    }

    async function handleSaveSettings() {
        var settings = {
            language: $('#setLanguage').value,
            theme: $('#setTheme').value,
            defaultRunUser: $('#setDefaultUser').value,
            defaultTimeout: parseInt($('#setDefaultTimeout').value) || 0,
            defaultMaxRetries: parseInt($('#setDefaultRetries').value) || 0,
            defaultRetryInterval: parseInt($('#setDefaultRetryInterval').value) || 60,
            autoRefreshInterval: parseInt($('#setAutoRefresh').value) || 15,
            logPageSize: parseInt($('#setLogPageSize').value) || 20,
            logMaxBytes: parseInt($('#setLogMaxBytes').value) || 10485760,
            logBackupCount: parseInt($('#setLogBackupCount').value) || 5,
            daemonLogLevel: $('#setDaemonLogLevel').value || 'all',
            daemonLogLines: parseInt($('#setDaemonLogLines').value) || 100,
            daemonLogInterval: parseInt($('#setDaemonLogInterval').value) || 2,
            daemonLogMaxBytes: parseInt($('#setDaemonLogMaxBytes').value) || 10485760,
            daemonLogBackupCount: parseInt($('#setDaemonLogBackupCount').value) || 3
        };

        try {
            await saveSettings(settings);
            closeSettingsModal();
            showToast(I18n.t('settings.saved'), 'success');

            // Apply language change
            var newLang = settings.language;
            if (newLang !== I18n.getLang()) {
                await I18n.switchLang(newLang);
                initLangSwitcher();
            }

            // Apply theme change
            if (settings.theme === 'auto') {
                localStorage.removeItem('cronplus-theme');
                Theme.init();
            } else {
                Theme.apply(settings.theme);
            }

            // Re-render with new page size
            logPage = 1;
            renderLogs();
        } catch (e) {
            showToast(I18n.t('settings.saveFailed'), 'error');
        }
    }

    async function handleResetSettings() {
        if (!confirm(I18n.t('settings.resetConfirm'))) return;
        try {
            await saveSettings(Object.assign({}, SETTINGS_DEFAULTS));
            openSettingsModal();  // Refresh the form
            showToast(I18n.t('settings.resetDone'), 'success');
        } catch (e) {
            showToast(I18n.t('settings.saveFailed'), 'error');
        }
    }

    // ===== Daemon Log Viewer =====
    var LOG_LEVEL_MAP = { DEBUG: 0, INFO: 1, WARNING: 2, ERROR: 3, CRITICAL: 4 };

    function parseLogLine(raw) {
        // Format: "2026-04-23 09:30:40,000 [cronplus.scheduler] INFO: message"
        var m = raw.match(/^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},?\d*)\s+\[([^\]]+)\]\s+(\w+):\s*(.*)/);
        if (m) {
            var level = m[3];
            var msg = m[4];
            // Smart level detection: override INFO if message indicates error/success
            var effective = _detectLevel(level, msg);
            return { ts: m[1], name: m[2], level: effective, rawLevel: level, msg: msg, raw: raw };
        }
        // Unmatched lines — try to detect level from content
        var effective2 = _detectLevel('INFO', raw);
        return { ts: '', name: '', level: effective2, rawLevel: 'INFO', msg: raw, raw: raw };
    }

    function _detectLevel(rawLevel, msg) {
        // If already ERROR/WARNING/CRITICAL, keep it
        if (rawLevel === 'ERROR' || rawLevel === 'CRITICAL') return rawLevel;
        if (rawLevel === 'WARNING') return rawLevel;
        // Detect from message content
        var lower = msg.toLowerCase();
        // Error indicators
        if (/[✗✕✘]|(\berror\b)|(\bfailed\b)|(\bfailure\b)|(\bexception\b)|(\btraceback\b)|(\berrno\b)|(\bexit[=: ]*(?!\s*0\b)[1-9])/i.test(msg)) {
            return 'ERROR';
        }
        // Warning indicators
        if (/\bwarning\b|\bwarn\b|\bstale\b|\btimeout\b|\bterminated\b|\bsigterm\b|\bsigkill\b/i.test(msg)) {
            return 'WARNING';
        }
        // Success indicators (keep as INFO but mark clearly)
        if (/[✓✔]|(\bsuccess\b)/i.test(msg)) {
            return 'INFO';
        }
        return rawLevel || 'INFO';
    }

    function _highlightMsg(msg) {
        // Escape HTML first, then apply highlights
        var safe = escHtml(msg);

        // Highlight error keywords — red
        safe = safe.replace(/(✗|✕|✘|error|failed|failure|exception|traceback|errno)/gi,
            '<span class="hl-error">$1</span>');
        // Highlight exit code (non-zero) — red
        safe = safe.replace(/\b(exit[=: ]*)([1-9]\d*)\b/gi,
            '<span class="hl-info">$1</span><span class="hl-error">$2</span>');
        // Highlight success keywords — green
        safe = safe.replace(/(✓|✔|success)/gi,
            '<span class="hl-success">$1</span>');
        // Highlight warning keywords — yellow
        safe = safe.replace(/(warning|warn|stale|timeout|terminated|SIGTERM|SIGKILL)/gi,
            '<span class="hl-warning">$1</span>');
        // Highlight task IDs — cyan
        safe = safe.replace(/\b(task\s*\[?\d+\]?)/gi,
            '<span class="hl-task">$1</span>');
        // Highlight PID numbers — dim cyan
        safe = safe.replace(/\b(pid[=: ]*\d+)\b/gi,
            '<span class="hl-pid">$1</span>');
        // Highlight duration — purple
        safe = safe.replace(/\((\d+ms)\)/g,
            '(<span class="hl-duration">$1</span>)');
        // Highlight attempt — dim
        safe = safe.replace(/\b(attempt[=: ]*\d+)\b/gi,
            '<span class="hl-attempt">$1</span>');

        return safe;
    }

    function renderDaemonLogs() {
        var viewer = $('#daemonLogViewer');
        if (!viewer) return;

        var levelFilter = ($('#daemonLogLevel') || {}).value || 'all';
        var maxLines = parseInt((($('#daemonLogLines') || {}).value) || '100') || 100;

        var filtered = daemonLogLines;
        if (levelFilter !== 'all') {
            var minLevel = LOG_LEVEL_MAP[levelFilter] || 0;
            filtered = filtered.filter(function (l) {
                return (LOG_LEVEL_MAP[l.level] || 0) >= minLevel;
            });
        }

        // Deduplicate "Next:" lines — keep only the latest per task ID
        var nextSeen = {};
        filtered = filtered.filter(function (l) {
            var m = l.msg.match(/^Next:\s*task\s*\[(\d+)\]/);
            if (m) {
                var tid = m[1];
                if (nextSeen[tid]) return false;
                nextSeen[tid] = true;
            }
            return true;
        });

        // Show last N lines
        var visible = filtered.slice(-maxLines);

        if (visible.length === 0) {
            viewer.innerHTML = '<div class="daemon-log-empty">' + I18n.t('daemon.noLogs') + '</div>';
        } else {
            var html = visible.map(function (l) {
                var colorClass = 'log-level-' + l.level;
                var ts = l.ts ? '<span class="log-ts">' + escHtml(l.ts) + '</span> ' : '';
                var name = l.name ? '<span class="log-name">[' + escHtml(l.name) + ']</span> ' : '';
                var levelBadge = '<span class="' + colorClass + '">' + escHtml(l.level) + ':</span> ';
                var msgHtml = _highlightMsg(l.msg);
                return '<div class="log-line">' + ts + name + levelBadge + msgHtml + '</div>';
            }).join('');
            viewer.innerHTML = html;
        }

        // Status
        var statusEl = $('#daemonLogStatus');
        var countEl = $('#daemonLogCount');
        if (statusEl) statusEl.textContent = I18n.t('daemon.linesShown', { shown: visible.length, total: filtered.length });
        if (countEl) countEl.textContent = '';

        // Auto scroll
        if (($('#daemonLogAutoScroll') || {}).checked !== false) {
            viewer.scrollTop = viewer.scrollHeight;
        }
    }

    function escHtml(s) {
        var d = document.createElement('div');
        d.textContent = s;
        return d.innerHTML;
    }

    async function loadDaemonLog() {
        var viewer = $('#daemonLogViewer');
        if (!viewer) return;
        try {
            var raw = await cockpit.spawn(
                ['bash', '-c', 'cat ' + Utils.shellQuote(DAEMON_LOG_FILE) + ' 2>/dev/null | tail -2000'],
                { err: 'message', environ: ['LC_ALL=C'] }
            );
            var lines = (raw || '').split('\n').filter(function (l) { return l.trim(); });
            daemonLogLines = lines.map(parseLogLine);
            renderDaemonLogs();
        } catch (e) {
            daemonLogLines = [{ ts: '', name: '', level: 'ERROR', msg: I18n.t('daemon.loadFailed') + ': ' + (e.message || e), raw: '' }];
            renderDaemonLogs();
        }
    }

    function startDaemonLogTimer() {
        stopDaemonLogTimer();
        if (($('#daemonLogAutoRefresh') || {}).checked === false) return;
        var interval = (parseInt((($('#daemonLogInterval') || {}).value)) || appSettings.daemonLogInterval || 2) * 1000;
        daemonLogTimer = setInterval(function () {
            if (($('#daemonLogAutoRefresh') || {}).checked !== false) {
                loadDaemonLog();
            }
        }, interval);
    }

    function stopDaemonLogTimer() {
        if (daemonLogTimer) { clearInterval(daemonLogTimer); daemonLogTimer = null; }
    }

    // ===== Language Switcher (now only in settings modal) =====
    function initLangSwitcher() {
        // Language is managed via settings modal only
    }

    // ===== Footer =====
    var footerStatusTimer = null;

    function initFooter() {
        // Load version from manifest.json
        Utils.shellReadJson('/usr/share/cockpit/cronplus/manifest.json').then(function (m) {
            var ver = m && m.plugin_version ? m.plugin_version : '?';
            $('#footerVersion').textContent = 'v' + ver;
        }).catch(function () {
            // Fallback: try relative path
            cockpit.file('manifest.json').read().then(function (content) {
                try {
                    var m = JSON.parse(content);
                    $('#footerVersion').textContent = 'v' + (m.plugin_version || '?');
                } catch (e) { $('#footerVersion').textContent = 'v?'; }
            }).catch(function () { $('#footerVersion').textContent = 'v?'; }
            );
        });

        // Initial status check + periodic poll
        updateFooterStatus();
        footerStatusTimer = setInterval(updateFooterStatus, 10000);
    }

    function updateFooterStatus() {
        var el = $('#footerStatus');
        if (!el) return;
        cockpit.spawn(
            ['systemctl', 'is-active', 'cronplus.service'],
            { err: 'message', environ: ['LC_ALL=C'] }
        ).then(function (out) {
            var state = (out || '').trim();
            if (state === 'active') {
                el.className = 'footer-status running';
                el.querySelector('.status-text').textContent = 'running';
            } else {
                el.className = 'footer-status stopped';
                el.querySelector('.status-text').textContent = state || 'stopped';
            }
        }).catch(function () {
            el.className = 'footer-status stopped';
            el.querySelector('.status-text').textContent = 'stopped';
        });
    }

    // ===== Auto Refresh =====
    function startAutoRefresh() {
        stopAutoRefresh();
        autoRefreshTimer = setInterval(autoRefresh, AUTO_REFRESH_MS);
    }

    function stopAutoRefresh() {
        if (autoRefreshTimer) { clearInterval(autoRefreshTimer); autoRefreshTimer = null; }
    }

    async function autoRefresh() {
        if (refreshing) return;
        if ($('#taskModal').style.display !== 'none') return;
        if ($('#outputModal').style.display !== 'none') return;
        if ($('#cleanupModal').style.display !== 'none') return;
        if ($('#settingsModal').style.display !== 'none') return;
        if (userInteracting) return;
        if (document.activeElement && /^(INPUT|SELECT|TEXTAREA)$/.test(document.activeElement.tagName)) return;
        if (document.querySelector('.log-entry.expanded')) return;
        refreshing = true;
        try {
            await loadTasks();
            await loadLogs();
            var activeTab = document.querySelector('.tab.active');
            if (activeTab) {
                if (activeTab.dataset.tab === 'tasks') renderTasks();
                if (activeTab.dataset.tab === 'logs') renderLogs();
            }
        } catch (e) { /* silent */ }
        finally { refreshing = false; }
    }

    // ===== System Users =====
    async function loadSystemUsers() {
        try {
            var out = await Utils.spawn('bash', ['-c', "getent passwd | awk -F: '$3>=1000 && $3<65534{print $1}'"]);
            var allUsers = out.trim().split('\n').filter(Boolean);
            systemUsers = ['root'].concat(allUsers.filter(function (u) { return u !== 'root'; }));
        } catch (e) { systemUsers = ['root']; }
        if (systemUsers.indexOf('root') < 0) systemUsers.unshift('root');
        populateUserSelects();
    }

    function populateUserSelects() {
        var el = $('#inputUser');
        if (!el) return;
        var val = el.value;
        el.innerHTML = '';
        systemUsers.forEach(function (u) {
            el.innerHTML += '<option value="' + Utils.escHtml(u) + '">' + Utils.escHtml(u) + '</option>';
        });
        el.innerHTML += '<option value="__custom__">' + I18n.t('form.customUser') + '</option>';
        if (val && systemUsers.indexOf(val) >= 0) el.value = val;
        else if (val === '__custom__') { el.value = '__custom__'; toggleCustomUser(true); }
        else if (val) { el.value = '__custom__'; $('#inputCustomUser').value = val; toggleCustomUser(true); }
    }

    function toggleCustomUser(show) {
        var sel = $('#inputUser');
        var input = $('#inputCustomUser');
        if (show === undefined) show = input.style.display === 'none';
        if (show) {
            sel.style.display = 'none';
            input.style.display = '';
            input.focus();
        } else {
            sel.style.display = '';
            input.style.display = 'none';
        }
    }

    function getSelectedUser() {
        var sel = $('#inputUser');
        var custom = $('#inputCustomUser');
        if (sel.value === '__custom__') return (custom.value || '').trim() || 'root';
        return sel.value || 'root';
    }

    async function loadUser() {
        try {
            var user = await Utils.spawn('whoami');
            $('#currentUser').textContent = user.trim();
        } catch (e) { $('#currentUser').textContent = 'unknown'; }
    }

    // ===== Load / Save Tasks =====
    async function loadTasks() {
        try {
            var raw = await Utils.shellReadJson(CONF_FILE);
            tasks = raw.map(Utils.decodeTaskOnLoad);
        } catch (err) {
            showToast(I18n.t('task.loadFailed') + ': ' + err.message, 'error');
        }
    }

    async function saveTasks() {
        try {
            var encoded = tasks.map(Utils.encodeTaskForSave);
            await Utils.shellWriteJson(CONF_FILE, encoded);
            // Signal daemon to reload config (SIGHUP = hot reload, no restart)
            try {
                await cockpit.spawn(
                    ['bash', '-c',
                     'PID=$(systemctl show -p MainPID --value cronplus 2>/dev/null); ' +
                     'if [ -n "$PID" ] && [ "$PID" -gt 0 ] 2>/dev/null; then ' +
                     '  kill -HUP $PID && echo "SIGHUP sent to pid $PID"; ' +
                     'else ' +
                     '  systemctl restart cronplus 2>/dev/null || true; fi'],
                    { err: 'message', environ: ['LC_ALL=C'] }
                );
            } catch (e) { /* signal best-effort */ }
            showToast(I18n.t('task.saved'), 'success');
        } catch (err) {
            showToast(I18n.t('task.saveFailed') + ': ' + err.message, 'error');
        }
    }

    // Silent save without toast (for internal state updates like run_seq reset)
    async function saveTasksQuiet() {
        try {
            var encoded = tasks.map(Utils.encodeTaskForSave);
            await Utils.shellWriteJson(CONF_FILE, encoded);
        } catch (e) { /* silent */ }
    }

    async function loadLogs() {
        try {
            var raw = await cockpit.spawn(
                ['cronplus', 'logs', '--all', '--json', '-n', '1000'],
                { err: 'message', environ: ['LC_ALL=C'] }
            );
            logs = JSON.parse(raw || '[]') || [];
            if (!Array.isArray(logs)) logs = [];
        } catch (e) { logs = []; }
    }

    // ===== Last Run Info (time + status) =====
    function getLastRunInfo(taskId) {
        var last = null;
        var lastStatus = null;
        logs.forEach(function (l) {
            if (l.task_id === taskId) {
                if (!last || l.created_at > last) {
                    last = l.created_at;
                    lastStatus = l.status;
                }
            }
        });
        return { time: last, status: lastStatus };
    }

    // ===== Estimate interval from cron schedule (for end time calculation) =====
    function _estimateIntervalMs(parsed) {
        // parsed: {sec, min, hour, day, month, dow} — each a string
        var sec = parsed.sec || '0';
        var min = parsed.min || '*';
        var hour = parsed.hour || '*';
        var day = parsed.day || '*';
        if (day !== '*' || hour !== '*') {
            // Daily or hourly schedule
            if (day !== '*') return 86400000; // ~1 day
            if (hour !== '*') return 3600000;  // ~1 hour
        }
        // Try to extract interval from */N patterns
        var minMatch = min.match(/^\*\/(\d+)$/);
        if (minMatch) return parseInt(minMatch[1]) * 60000;
        var secMatch = sec.match(/^\*\/(\d+)$/);
        if (secMatch) return parseInt(secMatch[1]) * 1000;
        // Fixed minute
        if (min !== '*' && hour === '*') return 3600000; // every hour at this minute
        if (min !== '*' && hour !== '*') return 86400000; // every day
        // Default: every minute
        return 60000;
    }

    // ===== Render Tasks =====
    function renderTasks() {
        var container = $('#taskList');
        var search = ($('#searchInput')?.value || '').toLowerCase();

        var filtered = tasks.filter(function (t) {
            if (!search) return true;
            return (t.command || '').toLowerCase().includes(search) ||
                (t.title || '').toLowerCase().includes(search) ||
                (t.comment || '').toLowerCase().includes(search) ||
                (t.run_user || '').toLowerCase().includes(search) ||
                (t.cwd || '').toLowerCase().includes(search) ||
                (t.schedule || '').toLowerCase().includes(search) ||
                (t.tags || '').toLowerCase().includes(search);
        });

        if (filtered.length === 0) {
            container.innerHTML =
                '<div class="empty-state">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">' +
                '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>' +
                '<p data-i18n="' + (search ? 'task.noMatch' : 'task.noTasks') + '">' +
                (search ? I18n.t('task.noMatch') : I18n.t('task.noTasks')) + '</p>' +
                '<span data-i18n="' + (search ? 'task.noMatchHint' : 'task.noTasksHint') + '">' +
                (search ? I18n.t('task.noMatchHint') : I18n.t('task.noTasksHint')) + '</span></div>';
            return;
        }

        container.innerHTML = filtered.map(function (task) {
            var idx = tasks.indexOf(task);
            var cmdDisplay = (task.command || '');
            var firstLine = cmdDisplay.split('\n')[0];
            var hasMultipleLines = cmdDisplay.indexOf('\n') >= 0;
            if (firstLine.length > 80) firstLine = firstLine.slice(0, 77) + '...';
            if (hasMultipleLines) firstLine += ' ' + I18n.t('task.multiLine');
            var lastRunInfo = getLastRunInfo(task.id);
            var lastRunShort = lastRunInfo.time ? TimeUtil.short(lastRunInfo.time) : null;
            var lastRunFailed = lastRunInfo.status === 'error';
            var lastRunIcon = '';
            if (lastRunInfo.status === 'success') {
                lastRunIcon = '<span class="last-run-icon success" title="' + I18n.t('log.filter.success') + '">' +
                    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" width="14" height="14">' +
                    '<polyline points="20 6 9 17 4 12"/></svg></span>';
            } else if (lastRunInfo.status === 'error') {
                lastRunIcon = '<span class="last-run-icon error" title="' + I18n.t('log.filter.error') + '">' +
                    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" width="14" height="14">' +
                    '<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></span>';
            }

            var nextRunStr = '';
            var nextRunDate = null;
            if (task.enabled !== false && task.schedule && !task.schedule.startsWith('@')) {
                var p = CronUtil.parseSchedule(task.schedule);
                var next = CronUtil.getNextRunTime(p.sec, p.min, p.hour, p.day, p.month, p.dow);
                if (next) {
                    nextRunDate = next;
                    nextRunStr = TimeUtil.full(next);
                }
            } else if (task.schedule && task.schedule.startsWith('@')) {
                nextRunStr = task.schedule;
            }

            // Row 3: task params (badges)
            var params = '';
            if (task.schedule) params += '<span class="task-schedule">' + Utils.escHtml(task.schedule) + '</span>';
            params += '<span class="task-user">' + Utils.escHtml(task.run_user || 'root') + '</span>';
            if (task.cwd) params += '<span class="task-cwd" title="工作目录"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg> ' + Utils.escHtml(task.cwd) + '</span>';
            if (task.timeout > 0) params += '<span class="badge badge-info"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg> ' + task.timeout + 's</span>';
            if (task.max_retries > 0) params += '<span class="badge badge-info"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg> ' + task.max_retries + '</span>';
            if (task.tags) params += '<span class="badge badge-tags">' + Utils.escHtml(task.tags) + '</span>';
            if (task.comment) params += '<span class="task-comment">' + Utils.escHtml(task.comment) + '</span>';

            // Row 4: time info + remaining runs
            var timeRow = '';
            // Last run
            timeRow += '<span class="time-item"><span class="time-label" data-i18n="task.lastRun">' + I18n.t('task.lastRun') + '</span> ';
            if (lastRunShort) {
                timeRow += lastRunIcon + '<span class="time-last' + (lastRunFailed ? ' failed' : '') + '">' + Utils.escHtml(lastRunShort) + '</span>';
            } else {
                timeRow += '<span class="time-na" data-i18n="task.noRecord">' + I18n.t('task.noRecord') + '</span>';
            }
            timeRow += '</span>';
            // Next run
            timeRow += '<span class="time-item"><span class="time-label" data-i18n="task.nextRun">' + I18n.t('task.nextRun') + '</span> ';
            if (nextRunStr) {
                timeRow += '<span class="time-next">' + Utils.escHtml(nextRunStr) + '</span>';
            } else {
                timeRow += '<span class="time-na">-</span>';
            }
            timeRow += '</span>';
            // Remaining runs & end time (if task has max_runs)
            if (task.max_runs > 0) {
                var remaining = Math.max(0, task.max_runs - (task.run_count || 0));
                timeRow += '<span class="time-sep">|</span>';
                timeRow += '<span class="time-item"><span class="time-remaining">' + remaining + '</span><span class="time-label"> / ' + task.max_runs + ' ' + I18n.t('task.remaining') + '</span></span>';
                if (remaining > 0 && nextRunDate) {
                    // Estimate end time: remaining runs * interval
                    var p2 = CronUtil.parseSchedule(task.schedule);
                    var intervalMs = _estimateIntervalMs(p2);
                    if (intervalMs > 0) {
                        var endDate = new Date(nextRunDate.getTime() + intervalMs * (remaining - 1));
                        timeRow += '<span class="time-item"><span class="time-label">' + I18n.t('task.endTime') + '</span> <span class="time-end">' + Utils.escHtml(TimeUtil.full(endDate)) + '</span></span>';
                    }
                } else if (remaining === 0) {
                    timeRow += '<span class="time-item"><span class="time-label">' + I18n.t('task.completed') + '</span></span>';
                }
            }

            return '<div class="task-card ' + (task.enabled === false ? 'disabled' : '') + '" data-index="' + idx + '">' +
                '<label class="task-toggle">' +
                '<input type="checkbox" ' + (task.enabled !== false ? 'checked' : '') + ' data-action="toggle" data-index="' + idx + '">' +
                '<span class="toggle-slider"></span></label>' +
                '<div class="task-info">' +
                // Row 1: ID + Title
                '<div class="task-title"><span class="task-id-badge">#' + task.id + '</span>' + (task.title ? Utils.escHtml(task.title) : '') + '</div>' +
                // Row 2: Command excerpt
                '<div class="task-command" title="' + Utils.escHtml(task.command || '') + '">' + Utils.escHtml(firstLine) + '</div>' +
                // Row 3: Params
                '<div class="task-params">' + params + '</div>' +
                // Row 4: Time info
                '<div class="task-time-info">' + timeRow + '</div>' +
                '</div>' +
                '<div class="task-actions">' +
                '<button class="btn btn-sm btn-secondary" data-action="run" data-index="' + idx + '" data-i18n-title="btn.run">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"/></svg>' +
                '<span data-i18n="btn.run">' + I18n.t('btn.run') + '</span></button>' +
                '<button class="btn btn-sm btn-log" data-action="logs" data-index="' + idx + '" data-i18n-title="btn.logs">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">' +
                '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>' +
                '<polyline points="14 2 14 8 20 8"/></svg>' +
                '<span data-i18n="btn.logs">' + I18n.t('btn.logs') + '</span></button>' +
                '<button class="btn btn-sm btn-secondary" data-action="edit" data-index="' + idx + '" data-i18n-title="btn.edit">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg></button>' +
                '<button class="btn btn-sm btn-danger" data-action="delete" data-index="' + idx + '" data-i18n-title="btn.delete">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg></button>' +
                '</div></div>';
        }).join('');
    }

    // ===== Render Logs =====
    function renderLogs(filterCommand) {
        var container = $('#logList');
        var filterTask = filterCommand || ($('#logFilterTask')?.value || '');
        var filterStatus = $('#logFilterStatus')?.value || '';
        var filterUser = $('#logFilterUser')?.value || '';
        var filterTrigger = $('#logFilterTrigger')?.value || '';
        var dateFrom = ($('#logDateFrom')?.value || '').trim();
        var dateTo = ($('#logDateTo')?.value || '').trim();

        var pageSizeEl = $('#logPageSize');
        var pageSize = LOG_PAGE_SIZE;
        if (pageSizeEl) {
            pageSize = parseInt(pageSizeEl.value) || LOG_PAGE_SIZE;
            LOG_PAGE_SIZE = pageSize;
        }

        var filtered = logs.filter(function (l) {
            if (filterTask && String(l.task_id) !== String(filterTask) && l.command !== filterTask) return false;
            if (filterStatus && l.status !== filterStatus) return false;
            if (filterUser && (l.run_user || 'root') !== filterUser) return false;
            if (filterTrigger && (l.trigger || 'auto') !== filterTrigger) return false;
            if (dateFrom || dateTo) {
                var logDate = (l.created_at || '').slice(0, 10);
                if (dateFrom && logDate < dateFrom) return false;
                if (dateTo && logDate > dateTo) return false;
            }
            return true;
        });

        filtered.sort(function (a, b) { return (b.created_at || '').localeCompare(a.created_at || ''); });

        // Populate task filter dropdown
        var taskFilter = $('#logFilterTask');
        if (taskFilter && !filterCommand) {
            var seen = {};
            var currentVal = taskFilter.value;
            taskFilter.innerHTML = '<option value="">' + I18n.t('log.filter.allTasks') + '</option>';
            logs.forEach(function (l) {
                var key = l.task_id || l.command;
                if (!seen[key]) {
                    seen[key] = true;
                    var label = '#' + (l.task_id || '?') + ' ' + (l.title || (l.command || '').slice(0, 50));
                    taskFilter.innerHTML += '<option value="' + Utils.escHtml(String(l.task_id || '')) + '">' + Utils.escHtml(label) + '</option>';
                }
            });
            taskFilter.value = currentVal;
        }

        // Populate user filter dropdown
        var userFilter = $('#logFilterUser');
        if (userFilter) {
            var currentUserVal = userFilter.value;
            var userSet = {};
            logs.forEach(function (l) { userSet[l.run_user || 'root'] = true; });
            userFilter.innerHTML = '<option value="">' + I18n.t('log.filter.allUsers') + '</option>';
            Object.keys(userSet).sort().forEach(function (u) {
                userFilter.innerHTML += '<option value="' + Utils.escHtml(u) + '">' + Utils.escHtml(u) + '</option>';
            });
            userFilter.value = currentUserVal;
        }

        // Filter info
        var filterInfo = $('#logFilterInfo');
        var hasFilter = filterTask || filterStatus || filterUser || filterTrigger || dateFrom || dateTo;
        if (filterInfo) {
            if (hasFilter && filtered.length > 0) {
                filterInfo.textContent = I18n.t('log.records', { count: filtered.length });
                filterInfo.style.display = '';
            } else {
                filterInfo.style.display = 'none';
            }
        }

        // Pagination
        var totalPages = Math.max(1, Math.ceil(filtered.length / LOG_PAGE_SIZE));
        if (logPage > totalPages) logPage = totalPages;
        var startIdx = (logPage - 1) * LOG_PAGE_SIZE;
        var pageItems = filtered.slice(startIdx, startIdx + LOG_PAGE_SIZE);

        if (filtered.length === 0) {
            container.innerHTML =
                '<div class="empty-state">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">' +
                '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>' +
                '<p>' + (hasFilter ? I18n.t('log.noMatch') : I18n.t('log.noLogs')) + '</p>' +
                '<span>' + (hasFilter ? I18n.t('log.noMatchHint') : I18n.t('log.noLogsHint')) + '</span></div>';
        } else {
            container.innerHTML = pageItems.map(function (log, i) {
                var globalIdx = startIdx + i;
                var statusLabel = log.status === 'success' ? I18n.t('log.filter.success') : I18n.t('log.filter.error');
                var durationLabel = log.duration ? log.duration + 'ms' : '';
                var attemptLabel = log.attempt > 1 ? ' (retry #' + log.attempt + ')' : '';
                var triggerLabel = log.trigger === 'manual' ? I18n.t('log.filter.manual') : I18n.t('log.filter.auto');
                var triggerClass = log.trigger === 'manual' ? 'trigger-manual' : 'trigger-auto';
                var hasOutput = log.output && log.output.trim();
                return '<div class="log-entry">' +
                    '<div class="log-status ' + log.status + '"></div>' +
                    '<div class="log-info">' +
                    (log.title ? '<div class="log-title">' + Utils.escHtml(log.title) + '</div>' : '') +
                    '<div class="log-meta">' +
                    '<span class="log-time">' + Utils.escHtml(log.created_at || '') + '</span>' +
                    (log.run_id ? '<span class="log-run-id">' + Utils.escHtml(log.run_id) + '</span>' : '') +
                    (log.task_id ? '<span class="log-task-id">#' + log.task_id + '</span>' : '') +
                    '<span class="log-user"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg> ' + Utils.escHtml(log.run_user || 'root') + '</span>' +
                    '<span class="log-trigger ' + triggerClass + '">' + triggerLabel + '</span>' +
                    '<span class="log-status-label ' + log.status + '">' + statusLabel + attemptLabel + '</span>' +
                    (durationLabel ? '<span class="log-duration"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg> ' + durationLabel + '</span>' : '') +
                    '</div>' +
                    '<div class="log-detail-bar">' +
                    '<div class="log-detail-header" data-expand="' + globalIdx + '">' +
                    '<span class="log-detail-label" data-i18n="log.label">' + I18n.t('log.label') + '</span>' +
                    '<span class="log-expand-toggle">\u25bc ' + I18n.t('log.expand').replace('▼ ', '') + '</span></div>' +
                    '<div class="log-detail-body">' +
                    '<div class="log-cmd-bar"><span class="log-cmd-label" data-i18n="log.cmdLabel">' + I18n.t('log.cmdLabel') + '</span>' +
                    '<code class="log-cmd-text">' + Utils.escHtml(log.command || '') + '</code>' +
                    '<button class="btn-copy" data-copy-text="' + Utils.escHtml(log.command || '') + '" title="' + I18n.t('btn.copy') + '"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg> ' + I18n.t('btn.copy') + '</button></div>' +
                    (hasOutput ? '<div class="log-output-wrapper"><div class="log-output">' + colorizeOutput(log.output) + '</div>' +
                        '<button class="btn-copy" data-copy-text="' + Utils.escHtml(log.output) + '" title="' + I18n.t('btn.copy') + '"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg> ' + I18n.t('btn.copy') + '</button></div>' : '') +
                    '</div></div>' +
                    '</div></div>';
            }).join('');
        }

        var paginationBar = $('#logPagination');
        if (paginationBar) paginationBar.style.display = filtered.length > 0 ? '' : 'none';
        var pageSizeWrap = document.querySelector('.pagination-size');
        if (pageSizeWrap) pageSizeWrap.style.display = filtered.length >= LOG_PAGE_SIZE ? '' : 'none';

        renderPagination(totalPages);
    }

    function renderPagination(totalPages) {
        var el = $('#logPaginationNav');
        if (!el) return;
        if (totalPages <= 1) { el.innerHTML = ''; return; }

        var html = '';
        html += '<button class="page-btn page-btn-text" data-page="1"' + (logPage <= 1 ? ' disabled' : '') + '>' + I18n.t('pagination.first') + '</button>';
        html += '<button class="page-btn page-btn-text" data-page="' + (logPage - 1) + '"' + (logPage <= 1 ? ' disabled' : '') + '>' + I18n.t('pagination.prev') + '</button>';
        var start = Math.max(1, logPage - 3);
        var end = Math.min(totalPages, logPage + 3);
        if (start > 1) {
            html += '<button class="page-btn" data-page="1">1</button>';
            if (start > 2) html += '<span class="page-ellipsis">\u2026</span>';
        }
        for (var p = start; p <= end; p++) {
            html += '<button class="page-btn' + (p === logPage ? ' active' : '') + '" data-page="' + p + '">' + p + '</button>';
        }
        if (end < totalPages) {
            if (end < totalPages - 1) html += '<span class="page-ellipsis">\u2026</span>';
            html += '<button class="page-btn" data-page="' + totalPages + '">' + totalPages + '</button>';
        }
        html += '<button class="page-btn page-btn-text" data-page="' + (logPage + 1) + '"' + (logPage >= totalPages ? ' disabled' : '') + '>' + I18n.t('pagination.next') + '</button>';
        html += '<button class="page-btn page-btn-text" data-page="' + totalPages + '"' + (logPage >= totalPages ? ' disabled' : '') + '>' + I18n.t('pagination.last') + '</button>';
        html += '<span class="page-info">' + I18n.t('pagination.pageInfo', { current: logPage, total: totalPages }) + '</span>';
        el.innerHTML = html;
    }

    // ===== Log Output Colorizer =====
    function colorizeOutput(text) {
        if (!text) return '';
        var lines = text.split('\n');
        return lines.map(function (line) {
            var lineClass = '';
            if (/error|fail|fatal|panic|exception|critical|拒绝|失败|错误/i.test(line)) lineClass = 'log-line-error';
            else if (/warn|warning|注意|警告|deprecated/i.test(line)) lineClass = 'log-line-warn';
            else if (/success|ok|done|complete|完成|成功|✓|✔/i.test(line)) lineClass = 'log-line-success';
            else if (/info|notice|提示|信息/i.test(line)) lineClass = 'log-line-info';
            else if (/^\s*[\$#>]/.test(line)) lineClass = 'log-line-cmd';

            var parts = [];
            var re = /(\d+\.?\d*)|("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')|(\/[\w\-\.\/]+)/g;
            var lastIdx = 0;
            var m;
            while ((m = re.exec(line)) !== null) {
                if (m.index > lastIdx) parts.push(Utils.escHtml(line.substring(lastIdx, m.index)));
                if (m[1]) parts.push('<span class="log-num">' + Utils.escHtml(m[1]) + '</span>');
                else if (m[2]) parts.push('<span class="log-str">' + Utils.escHtml(m[2]) + '</span>');
                else if (m[3]) parts.push('<span class="log-path">' + Utils.escHtml(m[3]) + '</span>');
                lastIdx = m.index + m[0].length;
            }
            if (lastIdx < line.length) parts.push(Utils.escHtml(line.substring(lastIdx)));
            var inner = parts.join('');
            if (lineClass) return '<span class="log-line ' + lineClass + '">' + inner + '</span>';
            return inner;
        }).join('\n');
    }

    // ===== Actions =====
    async function runTask(index) {
        var task = tasks[index];
        if (!task || !task.id) return;
        pendingRunTask = task;
        showOutputModal(task.command, task.run_user || 'root');
    }

    function startRunOutput() {
        var task = pendingRunTask;
        if (!task) return;

        var outputEl = $('#outputContent');
        var statusEl = $('#outputStatus');
        var durationEl = $('#outputDuration');
        var btn = $('#btnRunStopOutput');

        outputEl.textContent = '';
        runStartTime = Date.now();

        // Update UI: running state
        statusEl.className = 'terminal-status running';
        statusEl.innerHTML = '<span class="spinner-terminal"></span> <span class="run-id">' + currentRunID + '</span> <span>' + I18n.t('output.executing') + '</span> <span class="trigger-badge-manual">[manual]</span>';
        btn.textContent = I18n.t('btn.stop');
        btn.className = 'btn btn-danger btn-sm';
        $('#btnCopyOutput').title = I18n.t('btn.copyOutput');
        durationEl.textContent = '';

        // Duration timer
        runDurationTimer = setInterval(function () {
            if (runStartTime) {
                var elapsed = ((Date.now() - runStartTime) / 1000).toFixed(1);
                durationEl.textContent = elapsed + 's';
            }
        }, 100);

        // Build the shell command
        var command = task.command || '';
        var shellCmd;
        if (task.run_user && task.run_user !== 'root') {
            shellCmd = ['su', '-', task.run_user, '-c', command];
        } else {
            shellCmd = ['bash', '-c', command];
        }

        var spawnOpts = { err: 'out', environ: ['LC_ALL=C'] };
        if (task.cwd) spawnOpts.directory = task.cwd;

        try {
            var proc = cockpit.spawn(shellCmd, spawnOpts);
            runningProc = proc;

            proc.stream(function (data) {
                outputEl.textContent += data;
                outputEl.scrollTop = outputEl.scrollHeight;
            });

            // Timeout handling
            if (task.timeout > 0) {
                runTimeoutTimer = setTimeout(function () {
                    if (runningProc) {
                        outputEl.textContent += '\n[cronplus] timeout (' + task.timeout + 's), terminating...\n';
                        try { runningProc.close('kill'); } catch (e) { /* */ }
                    }
                }, task.timeout * 1000);
            }

            proc.then(function () {
                finishRun(task, command, 'success', '', 0);
            }, function (err) {
                var exitCode = err && err.exit_status ? err.exit_status : -1;
                finishRun(task, command, 'error', outputEl.textContent.slice(0, 50000), exitCode);
            });
        } catch (err) {
            finishRun(task, command, 'error', err.message || err.toString(), -1);
        }
    }

    function stopRunOutput() {
        if (runningProc) {
            try { runningProc.close('kill'); } catch (e) { /* */ }
            runningProc = null;
        }
        if (runTimeoutTimer) { clearTimeout(runTimeoutTimer); runTimeoutTimer = null; }
        if (runDurationTimer) { clearInterval(runDurationTimer); runDurationTimer = null; }

        var statusEl = $('#outputStatus');
        var durationEl = $('#outputDuration');
        var btn = $('#btnRunStopOutput');
        var duration = Date.now() - runStartTime;

        statusEl.className = 'terminal-status error';
        statusEl.innerHTML = '<span class="run-id">' + currentRunID + '</span> <span>✗</span> <span>' + I18n.t('btn.stop') + '</span>';
        durationEl.textContent = (duration / 1000).toFixed(1) + 's';
        btn.textContent = I18n.t('btn.run');
        btn.className = 'btn btn-primary btn-sm';

        var task = pendingRunTask;
        if (task) writeRunLog(task, task.command || '', 'error', '[cronplus] stopped by user', duration, -1);
        loadLogs().then(function () { renderTasks(); });
    }

    function finishRun(task, command, status, output, exitCode) {
        if (runTimeoutTimer) { clearTimeout(runTimeoutTimer); runTimeoutTimer = null; }
        if (runDurationTimer) { clearInterval(runDurationTimer); runDurationTimer = null; }

        var duration = Date.now() - runStartTime;
        runningProc = null;

        var statusEl = $('#outputStatus');
        var durationEl = $('#outputDuration');
        var btn = $('#btnRunStopOutput');

        durationEl.textContent = (duration / 1000).toFixed(1) + 's';

        if (status === 'success') {
            statusEl.className = 'terminal-status success';
            statusEl.innerHTML = '<span class="run-id">' + currentRunID + '</span> <span>✓</span> <span>' + I18n.t('output.success') + '</span>';
        } else {
            statusEl.className = 'terminal-status error';
            statusEl.innerHTML = '<span class="run-id">' + currentRunID + '</span> <span>✗</span> <span>' + I18n.t('output.failed') + '</span>';
        }

        // Button back to "运行" for re-run
        btn.textContent = I18n.t('btn.run');
        btn.className = 'btn btn-primary btn-sm';
        $('#btnCopyOutput').title = I18n.t('btn.copyOutput');

        writeRunLog(task, command, status, output, duration, exitCode);
        loadLogs().then(function () { renderTasks(); });
    }

    function writeRunLog(task, command, status, output, durationMs, exitCode) {
        var logFile = '/opt/cronplus/logs/task_' + task.id + '.json';
        var entry = {
            run_id: currentRunID || generateRunID(task),
            task_id: task.id,
            title: task.title || '',
            command: command,
            status: status,
            output: (output || '').slice(0, 50000),
            duration: durationMs,
            exit_code: exitCode,
            attempt: 1,
            trigger: 'manual',
            run_user: task.run_user || 'root',
            created_at: new Date().toISOString().replace('T', ' ').slice(0, 19)
        };
        var entryJson = JSON.stringify(entry);
        var safeLogFile = Utils.shellQuote(logFile);
        // Use jq for safe JSON append (fallback: python3, then node)
        var script =
            'LOG=' + safeLogFile + ' && mkdir -p "$(dirname "$LOG")" && ' +
            'if command -v jq >/dev/null 2>&1; then ' +
            '  if [ -f "$LOG" ]; then ' +
            '    jq --argjson e ' + Utils.shellQuote(entryJson) + ' ". + [$e] | .[-1000:]" "$LOG" > "$LOG.tmp" && mv "$LOG.tmp" "$LOG"; ' +
            '  else ' +
            '    echo "[" > "$LOG" && echo ' + Utils.shellQuote(entryJson) + ' >> "$LOG" && echo "]" >> "$LOG"; ' +
            '  fi; ' +
            'elif command -v python3 >/dev/null 2>&1; then ' +
            '  python3 -c ' + Utils.shellQuote("import json,sys;p=sys.argv[1];e=json.loads(sys.argv[2]);d=[]\ntry:\n  f=open(p);d=json.load(f);f.close()\nexcept:pass\nif not isinstance(d,list):d=[]\nd.append(e);d=d[-1000:]\nf=open(p,'w');json.dump(d,f,indent=2);f.close()") + ' "$LOG" ' + Utils.shellQuote(entryJson) + '; ' +
            'fi';
        cockpit.spawn(
            ['bash', '-c', script],
            { err: 'ignore', environ: ['LC_ALL=C'] }
        ).catch(function () {});
    }

    function showTaskLogs(index) {
        var task = tasks[index];
        if (!task) return;
        $$('.tab').forEach(function (t) { t.classList.remove('active'); });
        $$('.tab-content').forEach(function (c) { c.classList.remove('active'); });
        $$('.tab')[1].classList.add('active');
        $('#tab-logs').classList.add('active');
        logPage = 1;
        $('#logFilterTask').value = String(task.id || '');
        $('#logFilterStatus').value = '';
        $('#logFilterUser').value = '';
        $('#logFilterTrigger').value = '';
        $('#logDateFrom').value = '';
        $('#logDateTo').value = '';
        renderLogs();
    }

    function toggleTask(index) {
        var wasEnabled = tasks[index].enabled !== false;
        tasks[index].enabled = !wasEnabled;
        // Reset run_seq when re-enabling a task (关闭后再打开，序号重置)
        if (!wasEnabled && tasks[index].enabled) {
            tasks[index].run_seq = 0;
            tasks[index].run_count = 0;
        }
        saveTasks();
        renderTasks();
    }

    function deleteTask(index) {
        var task = tasks[index];
        if (!confirm(I18n.t('task.confirmDelete') + '\n' + (task.title ? task.title + ': ' : '') + task.command)) return;
        tasks.splice(index, 1);
        saveTasks();
        renderTasks();
    }

    function editTask(index) {
        editingIndex = index;
        var task = tasks[index];
        $('#modalTitle').textContent = I18n.t('modal.editTask');
        $('#inputTitle').value = task.title || '';
        $('#inputCommand').value = task.command || '';
        $('#inputComment').value = task.comment || '';
        $('#inputEnabled').checked = task.enabled !== false;
        $('#inputUser').value = task.run_user || 'root';

        var knownUsers = systemUsers;
        if (task.run_user && knownUsers.indexOf(task.run_user) < 0) {
            $('#inputUser').value = '__custom__';
            $('#inputCustomUser').value = task.run_user;
            toggleCustomUser(true);
        } else {
            $('#inputUser').value = task.run_user || 'root';
            toggleCustomUser(false);
        }
        $('#inputTimeout').value = task.timeout || 0;
        $('#inputMaxRetries').value = task.max_retries || 0;
        $('#inputRetryInterval').value = task.retry_interval || 60;
        $('#inputMaxConcurrent').value = task.max_concurrent || 1;
        $('#inputKillPrevious').checked = !!task.kill_previous;
        $('#inputMaxRuns').value = task.max_runs || 0;
        $('#inputRunCount').value = task.run_seq || task.run_count || 0;
        $('#inputTags').value = task.tags || '';
        $('#inputLogDays').value = task.log_retention_days || 0;
        $('#inputLogMax').value = task.log_max_entries || 0;
        $('#inputRebootDelay').value = task.reboot_delay || 0;
        $('#inputCwd').value = task.cwd || '';

        var envVars = task.env_vars || {};
        if (typeof envVars === 'string') { try { envVars = JSON.parse(envVars); } catch (e) { envVars = {}; } }
        $('#inputEnvVars').value = Object.keys(envVars).map(function (k) { return k + '=' + envVars[k]; }).join('\n');

        var cmdVal = task.command || '';
        var isScript = cmdVal.indexOf('\n') >= 0 || cmdVal.startsWith('#!') || cmdVal.length > 200;
        $('#inputCommand').rows = isScript ? 8 : 4;

        if ((task.schedule || '').startsWith('@reboot')) {
            $$('.preset-btn').forEach(function (b) { b.classList.remove('active'); });
            var mp = $('.preset-btn[data-preset="@reboot"]');
            if (mp) mp.classList.add('active');
            $('#cronSec').value = '0';
            $('#cronMin').value = task.schedule;
            $('#cronHour').value = ''; $('#cronDay').value = ''; $('#cronMonth').value = ''; $('#cronDow').value = '';
            $('#rebootDelayGroup').style.display = '';
            $('.cron-fields').style.display = 'none';
        } else {
            var cp = CronUtil.parseSchedule(task.schedule);
            $('#cronSec').value = cp.sec;
            $('#cronMin').value = cp.min;
            $('#cronHour').value = cp.hour;
            $('#cronDay').value = cp.day;
            $('#cronMonth').value = cp.month;
            $('#cronDow').value = cp.dow;
            $('#rebootDelayGroup').style.display = 'none';
            $('.cron-fields').style.display = '';
        }
        CronUtil.updatePreview();
        updateNextRuns();
        showModal(true);
    }

    function addNewTask() {
        editingIndex = -1;
        $('#modalTitle').textContent = I18n.t('modal.addTask');
        $('#inputTitle').value = ''; $('#inputCommand').value = '';
        $('#inputComment').value = ''; $('#inputEnabled').checked = true;
        // Use settings defaults
        var defaultUser = appSettings.defaultRunUser || 'root';
        $('#inputUser').value = defaultUser;
        $('#inputCustomUser').value = '';
        toggleCustomUser(false);
        $('#inputTimeout').value = appSettings.defaultTimeout || 0;
        $('#inputMaxRetries').value = appSettings.defaultMaxRetries || 0;
        $('#inputRetryInterval').value = appSettings.defaultRetryInterval || 60;
        $('#inputMaxConcurrent').value = 1;
        $('#inputKillPrevious').checked = false;
        $('#inputMaxRuns').value = 0;
        $('#inputRunCount').value = 0;
        $('#inputRunCount').setAttribute('data-run-seq', '0');
        $('#inputTags').value = '';
        $('#inputLogDays').value = 0; $('#inputLogMax').value = 0;
        $('#inputRebootDelay').value = 0;
        $('#rebootDelayGroup').style.display = 'none';
        $('.cron-fields').style.display = '';
        $('#inputEnvVars').value = 'PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin';
        $$('.preset-btn').forEach(function (b) { b.classList.remove('active'); });
        $('#inputCommand').rows = 4;
        $('#inputCommand').placeholder = I18n.t('form.commandPlaceholder');
        $('#cronSec').value = '0';
        $('#cronMin').value = '*'; $('#cronHour').value = '*'; $('#cronDay').value = '*';
        $('#cronMonth').value = '*'; $('#cronDow').value = '*';
        CronUtil.updatePreview();
        updateNextRuns();
        showModal(true);
    }

    function saveTask() {
        var command = $('#inputCommand').value.trim();
        if (!command) { showToast(I18n.t('task.enterCommand'), 'error'); return; }

        var sec = $('#cronSec').value.trim() || '0';
        var min = $('#cronMin').value.trim() || '*';
        var hour = $('#cronHour').value.trim() || '*';
        var day = $('#cronDay').value.trim() || '*';
        var month = $('#cronMonth').value.trim() || '*';
        var dow = $('#cronDow').value.trim() || '*';
        var schedule = min.startsWith('@') ? min : sec + ' ' + min + ' ' + hour + ' ' + day + ' ' + month + ' ' + dow;

        var envVars = {};
        ($('#inputEnvVars').value || '').split('\n').forEach(function (line) {
            line = line.trim();
            if (!line || line.startsWith('#')) return;
            var eq = line.indexOf('=');
            if (eq > 0) envVars[line.slice(0, eq)] = line.slice(eq + 1);
        });

        var data = {
            title: $('#inputTitle').value.trim(),
            command: command,
            schedule: schedule,
            enabled: $('#inputEnabled').checked,
            comment: $('#inputComment').value.trim(),
            run_user: getSelectedUser(),
            timeout: parseInt($('#inputTimeout').value) || 0,
            max_retries: parseInt($('#inputMaxRetries').value) || 0,
            retry_interval: parseInt($('#inputRetryInterval').value) || 60,
            max_concurrent: parseInt($('#inputMaxConcurrent').value) || 1,
            kill_previous: $('#inputKillPrevious').checked,
            max_runs: parseInt($('#inputMaxRuns').value) || 0,
            run_count: parseInt($('#inputRunCount').value) || 0,
            run_seq: parseInt($('#inputRunCount').value) || 0,
            tags: $('#inputTags').value.trim(),
            log_retention_days: parseInt($('#inputLogDays').value) || 0,
            log_max_entries: parseInt($('#inputLogMax').value) || 0,
            reboot_delay: parseInt($('#inputRebootDelay').value) || 0,
            env_vars: envVars,
            cwd: ($('#inputCwd').value || '').trim()
        };

        if (editingIndex >= 0) {
            data.id = tasks[editingIndex].id;
            tasks[editingIndex] = data;
        } else {
            var maxId = tasks.reduce(function (m, t) { return Math.max(m, t.id || 0); }, 0);
            data.id = maxId + 1;
            tasks.push(data);
        }

        saveTasks();
        renderTasks();
        showModal(false);
        showToast(editingIndex >= 0 ? I18n.t('task.updated') : I18n.t('task.added'), 'success');
    }

    // ===== Next Runs =====
    function updateNextRuns() {
        var $ = Utils.$;
        var sec = $('#cronSec').value.trim() || '0';
        var min = $('#cronMin').value.trim();
        var hour = $('#cronHour').value.trim();
        var day = $('#cronDay').value.trim();
        var month = $('#cronMonth').value.trim();
        var dow = $('#cronDow').value.trim();
        var container = $('#nextRuns');
        if (min.startsWith('@')) {
            container.innerHTML = '<div class="special-note">' + CronUtil.describeSpecial(min) + ' — ' + I18n.t('special.noPredict') + '</div>';
            return;
        }
        try {
            var runs = CronUtil.getNextRunTimes(sec, min, hour, day, month, dow);
            if (!runs.length) { container.innerHTML = '<div class="special-note">' + I18n.t('special.cannotCalc') + '</div>'; return; }
            container.innerHTML = runs.map(function (d, i) {
                return '<div class="next-run-item"><span class="run-index">#' + (i + 1) + '</span>' +
                    '<span>' + d.toLocaleString('zh-CN', { hour12: false }) + '</span>' +
                    '<span class="run-relative">' + TimeUtil.relative(d) + '</span></div>';
            }).join('');
        } catch (e) { container.innerHTML = '<div class="special-note">' + I18n.t('special.calcError') + '</div>'; }
    }

    // ===== Import / Export =====
    function exportConfig() {
        var exportTasks = tasks.map(function (t) {
            var copy = {};
            for (var k in t) { if (t.hasOwnProperty(k)) copy[k] = t[k]; }
            return copy;
        });
        var data = {
            version: '1.0.15',
            exportTime: new Date().toISOString(),
            source: 'cronplus',
            tasks: exportTasks
        };
        var json = JSON.stringify(data, null, 2);
        var blob = new Blob([json], { type: 'application/json' });
        var url = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url;
        a.download = 'cronplus-export-' + new Date().toISOString().slice(0, 10) + '.json';
        a.click();
        URL.revokeObjectURL(url);
        showToast(I18n.t('toast.exported'), 'success');
    }

    function importConfig(file) {
        var reader = new FileReader();
        reader.onload = function (e) {
            try {
                var data = JSON.parse(e.target.result);
                if (!data.tasks || !Array.isArray(data.tasks)) { showToast(I18n.t('toast.invalidFile'), 'error'); return; }
                var maxId = tasks.reduce(function (m, t) { return Math.max(m, t.id || 0); }, 0);
                var imported = 0;
                data.tasks.forEach(function (item) {
                    if (!item.command) return;
                    maxId++;
                    item.id = maxId;
                    item.enabled = item.enabled !== false;
                    item.run_user = item.run_user || 'root';
                    item.timeout = item.timeout || 0;
                    item.max_retries = item.max_retries || 0;
                    item.retry_interval = item.retry_interval || 60;
                    item.max_concurrent = item.max_concurrent || 1;
                    item.kill_previous = !!item.kill_previous;
                    item.env_vars = item.env_vars || {};
                    item.tags = item.tags || '';
                    item.title = item.title || '';
                    item.comment = item.comment || '';
                    item.reboot_delay = item.reboot_delay || 0;
                    if (!item.schedule && (item.minute || item.hour)) {
                        item.schedule = (item.second || '0') + ' ' + (item.minute || '*') + ' ' +
                            (item.hour || '*') + ' ' + (item.day || '*') + ' ' +
                            (item.month || '*') + ' ' + (item.dow || '*');
                    }
                    item.schedule = item.schedule || '0 * * * * *';
                    tasks.push(item);
                    imported++;
                });
                saveTasks();
                renderTasks();
                showToast(I18n.t('toast.imported', { count: imported }), 'success');
            } catch (err) { showToast(I18n.t('toast.importFailed') + ': ' + (err.message || err), 'error'); }
        };
        reader.readAsText(file);
    }

    // ===== Raw Editor =====
    async function loadRawEditor() {
        try {
            var out = await Utils.spawn('bash', ['-c', 'cat ' + Utils.shellQuote(CONF_FILE) + ' 2>/dev/null || echo "[]"']);
            $('#rawEditor').value = out;
            $('#rawStatus').textContent = '';
        } catch (e) { $('#rawEditor').value = ''; }
    }

    async function saveRawEditor() {
        var content = $('#rawEditor').value;
        try {
            JSON.parse(content);
            await Utils.spawn('bash', ['-c', 'printf %s ' + Utils.shellQuote(content) + ' | tee ' + Utils.shellQuote(CONF_FILE) + ' > /dev/null']);
            // Signal daemon to reload config (SIGHUP = hot reload, no restart)
            try {
                await cockpit.spawn(
                    ['bash', '-c',
                     'PID=$(systemctl show -p MainPID --value cronplus 2>/dev/null); ' +
                     'if [ -n "$PID" ] && [ "$PID" -gt 0 ] 2>/dev/null; then ' +
                     '  kill -HUP $PID && echo "SIGHUP sent to pid $PID"; ' +
                     'else ' +
                     '  systemctl restart cronplus 2>/dev/null || true; fi'],
                    { err: 'message', environ: ['LC_ALL=C'] }
                );
            } catch (e) { /* signal best-effort */ }
            $('#rawStatus').textContent = I18n.t('raw.saved');
            $('#rawStatus').className = 'status-text saved';
            showToast(I18n.t('toast.configSaved'), 'success');
            await loadTasks();
            renderTasks();
        } catch (err) {
            if (err instanceof SyntaxError) {
                $('#rawStatus').textContent = I18n.t('raw.jsonError');
                showToast(I18n.t('toast.jsonError') + ': ' + err.message, 'error');
            } else {
                $('#rawStatus').textContent = I18n.t('raw.saveFailed') + ': ' + (err.message || err);
                showToast(I18n.t('toast.saveFailed') + ': ' + (err.message || err), 'error');
            }
            $('#rawStatus').className = 'status-text error';
        }
    }

    // ===== UI Helpers =====
    function showModal(show) { $('#taskModal').style.display = show ? 'flex' : 'none'; }

    function generateRunID(task) {
        // Manual run: #taskID-user-manual-xxxx
        if (task) {
            var user = task.run_user || 'root';
            var hex = Math.floor(Math.random() * 0xFFFF).toString(16).padStart(4, '0');
            return '#' + task.id + '-' + user + '-manual-' + hex;
        }
        // Fallback
        var now = new Date();
        var pad = function (n, w) { var s = String(n); while (s.length < w) s = '0' + s; return s; };
        var ts = pad(now.getFullYear() % 100, 2) + pad(now.getMonth() + 1, 2) + pad(now.getDate(), 2) +
                 '-' + pad(now.getHours(), 2) + pad(now.getMinutes(), 2) + pad(now.getSeconds(), 2);
        var hex = Math.floor(Math.random() * 0xFFFF).toString(16).padStart(4, '0');
        return ts + '-' + hex;
    }

    function showOutputModal(command, user) {
        var outputEl = $('#outputContent');
        var statusEl = $('#outputStatus');
        var durationEl = $('#outputDuration');
        var btn = $('#btnRunStopOutput');
        var copyBtn = $('#btnCopyOutput');
        var taskNameEl = $('#outputTaskName');
        var task = pendingRunTask;
        currentRunID = generateRunID(task);
        outputEl.textContent = '$ ' + command + '\n';
        statusEl.className = 'terminal-status';
        statusEl.innerHTML = '<span class="run-id">' + currentRunID + '</span> <span class="trigger-badge-manual">[manual]</span> <span>' + (user || 'root') + '</span>';
        durationEl.textContent = '';
        btn.textContent = I18n.t('btn.run');
        btn.className = 'btn btn-primary btn-sm';
        copyBtn.title = I18n.t('btn.copyCommand');
        // Show task name in header
        if (taskNameEl) {
            var task = pendingRunTask;
            var name = '';
            if (task) name = task.title || task.command.split('\n')[0].slice(0, 60);
            taskNameEl.textContent = name ? '— ' + name : '';
        }
        runningProc = null;
        runStartTime = 0;
        $('#outputModal').style.display = 'flex';
    }

    function closeOutputModal() {
        if (runningProc) {
            try { runningProc.close('kill'); } catch (e) { /* */ }
            runningProc = null;
        }
        if (runTimeoutTimer) { clearTimeout(runTimeoutTimer); runTimeoutTimer = null; }
        if (runDurationTimer) { clearInterval(runDurationTimer); runDurationTimer = null; }
        runStartTime = 0;
        pendingRunTask = null;
        $('#outputModal').style.display = 'none';
    }

    function showToast(message, type) {
        var container = $('#toastContainer');
        var toast = document.createElement('div');
        toast.className = 'toast ' + (type || 'info');
        var icons = { success: '\u2713', error: '\u2717', info: '\u2139' };
        toast.innerHTML = '<span>' + (icons[type] || '\u2139') + '</span><span>' + Utils.escHtml(message) + '</span>';
        container.appendChild(toast);
        setTimeout(function () { toast.remove(); }, 4000);
    }

    // ===== Log Cleanup =====
    function openCleanupModal() {
        var taskSel = $('#cleanupTaskId');
        taskSel.innerHTML = '<option value="">' + I18n.t('cleanup.selectPlaceholder') + '</option>';
        var seen = {};
        logs.forEach(function (l) {
            var key = l.task_id;
            if (key && !seen[key]) {
                seen[key] = true;
                var label = l.title || (l.command || '').slice(0, 50);
                taskSel.innerHTML += '<option value="' + Utils.escHtml(String(key)) + '">' + Utils.escHtml(label) + ' (#' + key + ')</option>';
            }
        });

        var userSel = $('#cleanupUser');
        var userSet = {};
        logs.forEach(function (l) { userSet[l.run_user || 'root'] = true; });
        userSel.innerHTML = '<option value="">' + I18n.t('cleanup.selectPlaceholder') + '</option>';
        Object.keys(userSet).sort().forEach(function (u) {
            userSel.innerHTML += '<option value="' + Utils.escHtml(u) + '">' + Utils.escHtml(u) + '</option>';
        });

        $$('input[name="cleanupMode"]').forEach(function (r) { r.checked = r.value === 'age'; });
        $$('.cleanup-opt').forEach(function (el) { el.style.display = 'none'; });
        $('#cleanupOptAge').style.display = '';
        $('#cleanupDays').value = 30;
        $('#cleanupCount').value = 100;
        $('#cleanupModal').style.display = 'flex';
        setTimeout(function () { previewCleanup(); }, 50);
    }

    function closeCleanupModal() { $('#cleanupModal').style.display = 'none'; }

    function getCleanupMode() {
        var checked = document.querySelector('input[name="cleanupMode"]:checked');
        return checked ? checked.value : 'age';
    }

    function filterLogsForCleanup() {
        var mode = getCleanupMode();
        var now = new Date();
        if (mode === 'all') return logs.slice();
        return logs.filter(function (l) {
            if (mode === 'age') {
                var days = parseInt($('#cleanupDays').value) || 30;
                var cutoff = new Date(now.getTime() - days * 86400000);
                var logDate = new Date((l.created_at || '').replace(/\//g, '-'));
                return !isNaN(logDate.getTime()) && logDate < cutoff;
            }
            if (mode === 'count') return false;
            if (mode === 'task') {
                var taskId = $('#cleanupTaskId').value;
                return taskId && String(l.task_id) === taskId;
            }
            if (mode === 'status') {
                var status = $('#cleanupStatus').value;
                return status && l.status === status;
            }
            if (mode === 'user') {
                var user = $('#cleanupUser').value;
                return user && (l.run_user || 'root') === user;
            }
            return false;
        });
    }

    function filterLogsByCount() {
        var keepN = parseInt($('#cleanupCount').value) || 100;
        var byTask = {};
        logs.forEach(function (l, i) {
            var key = l.task_id || '__no_task__';
            if (!byTask[key]) byTask[key] = [];
            byTask[key].push(i);
        });
        var removeSet = {};
        Object.keys(byTask).forEach(function (key) {
            var indices = byTask[key];
            if (indices.length > keepN) {
                indices.slice(0, indices.length - keepN).forEach(function (idx) { removeSet[idx] = true; });
            }
        });
        return logs.filter(function (_, i) { return removeSet[i]; });
    }

    function previewCleanup() {
        var mode = getCleanupMode();
        var toRemove = mode === 'count' ? filterLogsByCount() : filterLogsForCleanup();
        $('#cleanupPreviewCount').textContent = toRemove.length;
        $('#cleanupPreviewTotal').textContent = logs.length;
        $('#cleanupPreview').style.display = 'flex';
    }

    function execCleanup() {
        var mode = getCleanupMode();

        if (mode === 'all') {
            if (!confirm(I18n.t('toast.cleanupConfirm', { count: logs.length }))) return;
            cockpit.spawn(['cronplus', 'clear-logs'], { err: 'message', environ: ['LC_ALL=C'] }).then(function () {
                logs = [];
                renderLogs();
                renderTasks();
                closeCleanupModal();
                showToast(I18n.t('toast.cleaned', { count: 'all' }), 'success');
            }).catch(function (err) {
                showToast(I18n.t('toast.cleanupFailed') + ': ' + (err.message || err), 'error');
            });
            return;
        }

        var toRemove = mode === 'count' ? filterLogsByCount() : filterLogsForCleanup();

        if (toRemove.length === 0) {
            showToast(I18n.t('toast.noCleanup'), 'info');
            return;
        }
        if (!confirm(I18n.t('toast.cleanupConfirm', { count: toRemove.length }))) return;

        // Group by task_id and clear per-task
        var taskIds = new Set();
        toRemove.forEach(function (l) { if (l.task_id) taskIds.add(l.task_id); });

        var promises = [];
        taskIds.forEach(function (tid) {
            promises.push(cockpit.spawn(['cronplus', 'clear-logs', String(tid)], { err: 'message', environ: ['LC_ALL=C'] }));
        });

        Promise.all(promises).then(function () {
            return loadLogs();
        }).then(function () {
            renderLogs();
            renderTasks();
            closeCleanupModal();
            showToast(I18n.t('toast.cleaned', { count: toRemove.length }), 'success');
        }).catch(function (err) {
            showToast(I18n.t('toast.cleanupFailed') + ': ' + (err.message || err), 'error');
        });
    }

    // ===== Events =====
    function bindEvents() {
        $$('.tab').forEach(function (tab) {
            tab.addEventListener('click', function () {
                $$('.tab').forEach(function (t) { t.classList.remove('active'); });
                $$('.tab-content').forEach(function (c) { c.classList.remove('active'); });
                tab.classList.add('active');
                $('#tab-' + tab.dataset.tab).classList.add('active');
                if (tab.dataset.tab === 'logs') { logPage = 1; loadLogs().then(function () { renderLogs(); }); }
                if (tab.dataset.tab === 'editor') loadRawEditor();
                if (tab.dataset.tab === 'daemon') { loadDaemonLog(); startDaemonLogTimer(); }
                if (tab.dataset.tab !== 'daemon') stopDaemonLogTimer();
            });
        });

        // Language switch is now only in Settings modal
        $('#btnRefresh').addEventListener('click', async function () {
            if (refreshing) return;
            refreshing = true;
            var btn = $('#btnRefresh');
            btn.style.opacity = '0.5';
            btn.style.pointerEvents = 'none';
            try {
                await loadTasks();
                await loadLogs();
                renderTasks();
                renderLogs();
                showToast(I18n.t('toast.refreshed'), 'info');
            } catch (err) {
                showToast(I18n.t('toast.refreshFailed') + ': ' + (err.message || err), 'error');
            } finally {
                btn.style.opacity = '';
                btn.style.pointerEvents = '';
                refreshing = false;
            }
        });

        $('#btnAddTask').addEventListener('click', addNewTask);
        $('#searchInput').addEventListener('input', renderTasks);
        $('#btnCloseModal').addEventListener('click', function () { showModal(false); });
        $('#btnCancelModal').addEventListener('click', function () { showModal(false); });
        $('#btnSaveTask').addEventListener('click', saveTask);

        $('#btnCopyCommand').addEventListener('click', function () {
            var text = $('#inputCommand').value || '';
            var btn = $('#btnCopyCommand');
            var origHtml = btn.innerHTML;
            navigator.clipboard.writeText(text).then(function () {
                btn.classList.add('copied');
                btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><polyline points="20 6 9 17 4 12"/></svg>';
                setTimeout(function () { btn.innerHTML = origHtml; btn.classList.remove('copied'); }, 1500);
            }).catch(function () {
                var ta = document.createElement('textarea');
                ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
                document.body.appendChild(ta); ta.select(); document.execCommand('copy');
                document.body.removeChild(ta);
                btn.classList.add('copied');
                btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><polyline points="20 6 9 17 4 12"/></svg>';
                setTimeout(function () { btn.innerHTML = origHtml; btn.classList.remove('copied'); }, 1500);
            });
        });

        $('#inputUser').addEventListener('change', function () {
            if (this.value === '__custom__') { toggleCustomUser(true); }
            else { toggleCustomUser(false); }
        });

        $('#btnCloseOutput').addEventListener('click', closeOutputModal);
        $('#btnCloseOutput2').addEventListener('click', closeOutputModal);
        $('#btnRunStopOutput').addEventListener('click', function () {
            if (runningProc) {
                stopRunOutput();
            } else {
                startRunOutput();
            }
        });
        $('#btnCopyOutput').addEventListener('click', function () {
            var outputEl = $('#outputContent');
            var text = outputEl.textContent || '';
            var btn = $('#btnCopyOutput');
            var origHtml = btn.innerHTML;
            var tip = runningProc ? I18n.t('btn.copyOutput') : I18n.t('btn.copyCommand');
            navigator.clipboard.writeText(text).then(function () {
                btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>';
                btn.title = I18n.t('btn.copied');
                setTimeout(function () { btn.innerHTML = origHtml; btn.title = tip; }, 1500);
            }).catch(function () {
                var ta = document.createElement('textarea');
                ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
                document.body.appendChild(ta); ta.select(); document.execCommand('copy');
                document.body.removeChild(ta);
                btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>';
                btn.title = I18n.t('btn.copied');
                setTimeout(function () { btn.innerHTML = origHtml; btn.title = tip; }, 1500);
            });
        });

        $('#btnExport').addEventListener('click', exportConfig);
        $('#btnTheme').addEventListener('click', function () {
            Theme.toggle();
            // Persist theme to settings
            var current = Theme.getEffective();
            appSettings.theme = current;
            Utils.shellWriteJson(SETTINGS_FILE, appSettings).catch(function () {});
        });
        $('#btnSettings').addEventListener('click', openSettingsModal);
        $('#btnCloseSettings').addEventListener('click', closeSettingsModal);
        $('#btnCancelSettings').addEventListener('click', closeSettingsModal);
        $('#btnSaveSettings').addEventListener('click', handleSaveSettings);
        $('#btnResetSettings').addEventListener('click', handleResetSettings);
        // Close settings modal on overlay click
        $('#settingsModal').addEventListener('click', function (e) {
            if (e.target === this) closeSettingsModal();
        });

        // Daemon log controls
        $('#btnDaemonRefresh').addEventListener('click', loadDaemonLog);
        $('#btnDaemonClearFile').addEventListener('click', function () {
            if (!confirm(I18n.t('daemon.clearLogConfirm'))) return;
            cockpit.spawn(
                ['bash', '-c', '> ' + Utils.shellQuote(DAEMON_LOG_FILE) + ' && echo cleared'],
                { err: 'message', environ: ['LC_ALL=C'] }
            ).then(function () {
                daemonLogLines = [];
                renderDaemonLogs();
                showToast(I18n.t('daemon.logCleared'), 'success');
            }).catch(function (err) {
                showToast(I18n.t('daemon.clearFailed') + ': ' + (err.message || err), 'error');
            });
        });
        $('#daemonLogLevel').addEventListener('change', renderDaemonLogs);
        $('#daemonLogLines').addEventListener('change', renderDaemonLogs);
        $('#daemonLogAutoRefresh').addEventListener('change', function () {
            if (this.checked) startDaemonLogTimer(); else stopDaemonLogTimer();
        });
        $('#daemonLogInterval').addEventListener('change', function () {
            if (($('#daemonLogAutoRefresh') || {}).checked) startDaemonLogTimer();
        });

        $('#btnImport').addEventListener('click', function () { $('#importFile').click(); });
        $('#importFile').addEventListener('change', function (e) { if (e.target.files.length > 0) { importConfig(e.target.files[0]); e.target.value = ''; } });

        $$('.preset-btn').forEach(function (btn) {
            btn.addEventListener('click', function () {
                $$('.preset-btn').forEach(function (b) { b.classList.remove('active'); });
                btn.classList.add('active');
                var p = btn.dataset.preset;
                if (!p) return; // skip reset button (no data-preset)
                if (p === '@reboot') {
                    $('#cronSec').value = '0'; $('#cronMin').value = p;
                    $('#cronHour').value = ''; $('#cronDay').value = '';
                    $('#cronMonth').value = ''; $('#cronDow').value = '';
                    $('#rebootDelayGroup').style.display = '';
                    $('.cron-fields').style.display = 'none';
                } else {
                    var ps = p.split(' ');
                    $('#cronSec').value = ps[0]; $('#cronMin').value = ps[1];
                    $('#cronHour').value = ps[2]; $('#cronDay').value = ps[3];
                    $('#cronMonth').value = ps[4]; $('#cronDow').value = ps[5];
                    $('#rebootDelayGroup').style.display = 'none';
                    $('.cron-fields').style.display = '';
                }
                CronUtil.updatePreview(); updateNextRuns();
            });
        });

        // Auto-fill: when a higher field changes from * to specific, set lower fields * → 0
        // Field hierarchy (high→low): month → day → hour → min → sec
        var cronFieldOrder = ['cronSec', 'cronMin', 'cronHour', 'cronDay', 'cronMonth', 'cronDow'];
        var cronFieldLower = {
            'cronMonth': ['cronDay', 'cronHour', 'cronMin', 'cronSec'],
            'cronDay':   ['cronHour', 'cronMin', 'cronSec'],
            'cronHour':  ['cronMin', 'cronSec'],
            'cronMin':   ['cronSec'],
            'cronSec':   [],
            'cronDow':   []
        };
        var cronFieldPrev = {}; // track previous values

        cronFieldOrder.forEach(function (id) {
            var el = $('#' + id);
            if (!el) return;
            cronFieldPrev[id] = el.value;
            el.addEventListener('focus', function () {
                cronFieldPrev[id] = el.value;
            });
            el.addEventListener('input', function () {
                $$('.preset-btn').forEach(function (b) { b.classList.remove('active'); });
                $('#rebootDelayGroup').style.display = 'none';
                $('.cron-fields').style.display = '';

                var prev = cronFieldPrev[id] || '*';
                var curr = el.value.trim();
                // Auto-fill: if changed from * to a specific value, set lower fields to 0
                if (prev === '*' && curr !== '*' && curr !== '' && cronFieldLower[id]) {
                    cronFieldLower[id].forEach(function (lowerId) {
                        var lowerEl = $('#' + lowerId);
                        if (lowerEl && lowerEl.value.trim() === '*') {
                            lowerEl.value = '0';
                        }
                    });
                }
                cronFieldPrev[id] = curr;

                CronUtil.updatePreview(); updateNextRuns();
            });
        });

        // Reset button: restore all cron fields to defaults
        $('#btnResetCron').addEventListener('click', function () {
            $$('.preset-btn').forEach(function (b) { b.classList.remove('active'); });
            $('#cronSec').value = '0';
            $('#cronMin').value = '*';
            $('#cronHour').value = '*';
            $('#cronDay').value = '*';
            $('#cronMonth').value = '*';
            $('#cronDow').value = '*';
            $('#rebootDelayGroup').style.display = 'none';
            $('.cron-fields').style.display = '';
            CronUtil.updatePreview(); updateNextRuns();
        });

        $('#taskList').addEventListener('click', function (e) {
            var btn = e.target.closest('[data-action]');
            if (!btn) return;
            var action = btn.dataset.action, index = parseInt(btn.dataset.index);
            switch (action) {
                case 'run': runTask(index); break;
                case 'edit': editTask(index); break;
                case 'delete': deleteTask(index); break;
                case 'logs': showTaskLogs(index); break;
            }
        });

        $('#taskList').addEventListener('change', function (e) {
            if (e.target.dataset.action === 'toggle') toggleTask(parseInt(e.target.dataset.index));
        });

        $('#logList').addEventListener('click', function (e) {
            var expand = e.target.closest('[data-expand]');
            if (expand) {
                var entry = expand.closest('.log-entry');
                entry.classList.toggle('expanded');
                var toggle = expand.querySelector('.log-expand-toggle');
                toggle.textContent = entry.classList.contains('expanded') ?
                    I18n.t('log.collapse') : I18n.t('log.expand');
            }
            var copyBtn = e.target.closest('.btn-copy');
            if (copyBtn) {
                var text = copyBtn.getAttribute('data-copy-text') || '';
                var origHtml = copyBtn.innerHTML;
                navigator.clipboard.writeText(text).then(function () {
                    copyBtn.classList.add('copied');
                    copyBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><polyline points="20 6 9 17 4 12"/></svg> ' + I18n.t('btn.copied');
                    setTimeout(function () { copyBtn.innerHTML = origHtml; copyBtn.classList.remove('copied'); }, 1500);
                }).catch(function () {
                    var ta = document.createElement('textarea');
                    ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
                    document.body.appendChild(ta); ta.select(); document.execCommand('copy');
                    document.body.removeChild(ta);
                    copyBtn.classList.add('copied');
                    copyBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><polyline points="20 6 9 17 4 12"/></svg> ' + I18n.t('btn.copied');
                    setTimeout(function () { copyBtn.innerHTML = origHtml; copyBtn.classList.remove('copied'); }, 1500);
                });
            }
        });

        ['logFilterTask', 'logFilterStatus', 'logFilterUser', 'logFilterTrigger'].forEach(function (id) {
            var el = $('#' + id);
            if (el) el.addEventListener('change', function () { logPage = 1; renderLogs(); });
        });
        ['logDateFrom', 'logDateTo'].forEach(function (id) {
            var el = $('#' + id);
            if (el) el.addEventListener('change', function () { logPage = 1; renderLogs(); });
        });

        $('#btnClearFilters').addEventListener('click', function () {
            $('#logFilterTask').value = '';
            $('#logFilterStatus').value = '';
            $('#logFilterUser').value = '';
            $('#logFilterTrigger').value = '';
            $('#logDateFrom').value = '';
            $('#logDateTo').value = '';
            logPage = 1;
            renderLogs();
        });

        $('#logPaginationNav').addEventListener('click', function (e) {
            var btn = e.target.closest('[data-page]');
            if (!btn || btn.disabled) return;
            var page = parseInt(btn.dataset.page);
            if (page >= 1) { logPage = page; renderLogs(); }
        });

        $('#logPageSize').addEventListener('change', function () { logPage = 1; renderLogs(); });

        // Cleanup Modal
        $('#btnCleanupLogs').addEventListener('click', function () { openCleanupModal(); });
        $('#btnCloseCleanup').addEventListener('click', function () { closeCleanupModal(); });
        $('#btnCancelCleanup').addEventListener('click', function () { closeCleanupModal(); });
        $('#btnExecCleanup').addEventListener('click', function () { execCleanup(); });

        $$('input[name="cleanupMode"]').forEach(function (radio) {
            radio.addEventListener('change', function () {
                $$('.cleanup-opt').forEach(function (el) { el.style.display = 'none'; });
                var mode = this.value;
                var optMap = { age: 'cleanupOptAge', count: 'cleanupOptCount', task: 'cleanupOptTask', status: 'cleanupOptStatus', user: 'cleanupOptUser' };
                if (optMap[mode]) $('#' + optMap[mode]).style.display = '';
                previewCleanup();
            });
        });

        ['cleanupDays', 'cleanupCount', 'cleanupTaskId', 'cleanupStatus', 'cleanupUser'].forEach(function (id) {
            var el = $('#' + id);
            if (el) {
                el.addEventListener('input', function () { previewCleanup(); });
                el.addEventListener('change', function () { previewCleanup(); });
            }
        });

        $('#cleanupModal').addEventListener('click', function (e) { if (e.target === $('#cleanupModal')) closeCleanupModal(); });

        $('#btnSaveRaw').addEventListener('click', saveRawEditor);
        $('#btnCopyRaw').addEventListener('click', function () {
            var content = $('#rawEditor').value || '';
            var btn = $('#btnCopyRaw');
            var origHtml = btn.innerHTML;
            navigator.clipboard.writeText(content).then(function () {
                btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><polyline points="20 6 9 17 4 12"/></svg> <span>' + I18n.t('btn.copied') + '</span>';
                btn.classList.add('copied');
                showToast(I18n.t('btn.copied'), 'success');
                setTimeout(function () { btn.innerHTML = origHtml; btn.classList.remove('copied'); }, 2000);
            }).catch(function () {
                var ta = document.createElement('textarea');
                ta.value = content; ta.style.position = 'fixed'; ta.style.opacity = '0';
                document.body.appendChild(ta); ta.select(); document.execCommand('copy');
                document.body.removeChild(ta);
                btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><polyline points="20 6 9 17 4 12"/></svg> <span>' + I18n.t('btn.copied') + '</span>';
                btn.classList.add('copied');
                showToast(I18n.t('btn.copied'), 'success');
                setTimeout(function () { btn.innerHTML = origHtml; btn.classList.remove('copied'); }, 2000);
            });
        });

        $('#taskModal').addEventListener('click', function (e) { if (e.target === $('#taskModal')) return; });
        $('#outputModal').addEventListener('click', function (e) { if (e.target === $('#outputModal')) closeOutputModal(); });
        document.addEventListener('keydown', function (e) { if (e.key === 'Escape') { showModal(false); closeOutputModal(); } });

        // Track user interaction to suppress auto-refresh
        function markInteracting() {
            userInteracting = true;
            clearTimeout(interactionTimer);
            interactionTimer = setTimeout(function () { userInteracting = false; }, 5000);
        }
        document.addEventListener('mousemove', markInteracting);
        document.addEventListener('mousedown', markInteracting);
        document.addEventListener('scroll', markInteracting, true);
        document.addEventListener('focusin', function () {
            if (/^(INPUT|SELECT|TEXTAREA)$/.test(document.activeElement.tagName)) {
                userInteracting = true;
                clearTimeout(interactionTimer);
            }
        });
        document.addEventListener('focusout', function () {
            clearTimeout(interactionTimer);
            interactionTimer = setTimeout(function () { userInteracting = false; }, 2000);
        });
    }

    // ===== Update cleanup modal labels after language switch =====
    function updateCleanupLabels() {
        // Re-populate static labels that reference I18n
        var cleanupRadioLabels = {
            'cleanupOptAge': { label: 'cleanup.ageLabel', hint: 'cleanup.ageHint' },
            'cleanupOptCount': { label: 'cleanup.countLabel', hint: 'cleanup.countHint' },
            'cleanupOptTask': { label: 'cleanup.taskLabel' },
            'cleanupOptStatus': { label: 'cleanup.statusLabel' },
            'cleanupOptUser': { label: 'cleanup.userLabel' }
        };
    }

})();
