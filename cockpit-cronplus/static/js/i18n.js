/**
 * i18n module — loads language JSON via relative fetch
 */
var I18n = (function () {
    var _currentLang = 'en';
    var _dict = {};
    var _cache = {};

    /**
     * Detect preferred language:
     *   saved choice > Cockpit session language > browser navigator > 'en'
     *
     * cockpit.language is the language the user picked in Cockpit itself. It
     * was ignored, so someone using a Chinese Cockpit on an English browser
     * got this plugin in English no matter what.
     */
    function detectLang() {
        var saved = localStorage.getItem('lang');
        if (saved) return saved;
        var ck = (window.cockpit && window.cockpit.language) ? String(window.cockpit.language) : '';
        var nav = (ck || navigator.language || navigator.userLanguage || '').toLowerCase();
        if (nav.startsWith('zh')) return 'zh-CN';
        if (nav.startsWith('ja')) return 'ja';
        if (nav.startsWith('ko')) return 'ko';
        if (nav.startsWith('fr')) return 'fr';
        if (nav.startsWith('de')) return 'de';
        if (nav.startsWith('es')) return 'es';
        if (nav.startsWith('ru')) return 'ru';
        if (nav.startsWith('pt')) return 'pt-BR';
        return 'en';
    }

    function load(lang) {
        _currentLang = lang || _currentLang;
        localStorage.setItem('lang', _currentLang);
        if (_cache[_currentLang]) {
            _dict = _cache[_currentLang];
            return Promise.resolve(_dict);
        }
        var url = 'lang/' + _currentLang + '.json';
        return fetch(url)
            .then(function (r) {
                if (!r.ok) throw new Error(r.status);
                return r.json();
            })
            .then(function (data) {
                _cache[_currentLang] = data;
                _dict = data;
                return _dict;
            })
            .catch(function (err) {
                console.error('[i18n] load error:', err);
                if (_currentLang !== 'en') {
                    _currentLang = 'en';
                    return load('en');
                }
                _dict = {};
                return _dict;
            });
    }

    function t(key, params) {
        // `|| key` treated an empty translation as missing. Some strings are
        // deliberately empty — the English schedule preview reads "At sec 15"
        // because its unit suffix is blank — so those keys rendered as the raw
        // key name ("schedule.atSec15schedule.secUnit") instead.
        var text = Object.prototype.hasOwnProperty.call(_dict, key) ? _dict[key] : key;
        if (text === null || text === undefined) text = key;
        if (params) {
            Object.keys(params).forEach(function (k) {
                text = String(text).replace('{' + k + '}', params[k]);
            });
        }
        return text;
    }

    function applyToDOM() {
        document.querySelectorAll('[data-i18n]').forEach(function (el) {
            el.textContent = t(el.getAttribute('data-i18n'));
        });
        document.querySelectorAll('[data-i18n-placeholder]').forEach(function (el) {
            el.placeholder = t(el.getAttribute('data-i18n-placeholder'));
        });
        document.querySelectorAll('[data-i18n-title]').forEach(function (el) {
            el.title = t(el.getAttribute('data-i18n-title'));
        });
    }

    function switchLang(lang) {
        return load(lang).then(function () {
            applyToDOM();
            document.documentElement.lang = lang;
        });
    }

    function getLang() {
        return _currentLang;
    }

    return {
        load: load,
        t: t,
        applyToDOM: applyToDOM,
        switchLang: switchLang,
        getLang: getLang,
        detectLang: detectLang,
        ready: function () { return Object.keys(_dict).length > 0; }
    };
})();
