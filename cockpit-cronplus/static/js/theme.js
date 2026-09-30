/**
 * Theme management
 */
var Theme = (function () {
    var THEME_KEY = 'cronplus-theme';

    function getEffectiveTheme() {
        var saved = localStorage.getItem(THEME_KEY);
        if (saved === 'light' || saved === 'dark') return saved;
        return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
    }

    function applyTheme(theme) {
        document.documentElement.setAttribute('data-theme', theme);
        var iconDark = Utils.$('#iconDark');
        var iconLight = Utils.$('#iconLight');
        if (iconDark) iconDark.style.display = theme === 'dark' ? '' : 'none';
        if (iconLight) iconLight.style.display = theme === 'light' ? '' : 'none';
    }

    function init() {
        applyTheme(getEffectiveTheme());
        window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', function () {
            if (!localStorage.getItem(THEME_KEY)) {
                applyTheme(getEffectiveTheme());
            }
        });
    }

    function toggle() {
        var current = getEffectiveTheme();
        var next = current === 'dark' ? 'light' : 'dark';
        localStorage.setItem(THEME_KEY, next);
        applyTheme(next);
    }

    return { init: init, apply: applyTheme, toggle: toggle, getEffective: getEffectiveTheme };
})();
