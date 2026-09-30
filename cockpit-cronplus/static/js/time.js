/**
 * Time formatting utilities
 */
var TimeUtil = (function () {

    function formatRelativeTime(date) {
        var diff = date.getTime() - new Date().getTime();
        var secs = Math.round(diff / 1000);
        if (secs < 60) return secs + '秒';
        var mins = Math.round(diff / 60000);
        if (mins < 60) return mins + '分钟';
        var hours = Math.floor(mins / 60);
        if (hours < 24) return hours + 'h' + (mins % 60) + 'm';
        var days = Math.floor(hours / 24);
        return days + 'd' + (hours % 24) + 'h';
    }

    function _formatDatetime(d) {
        var y = d.getFullYear();
        var mo = String(d.getMonth() + 1).padStart(2, '0');
        var dd = String(d.getDate()).padStart(2, '0');
        var h = String(d.getHours()).padStart(2, '0');
        var m = String(d.getMinutes()).padStart(2, '0');
        var s = String(d.getSeconds()).padStart(2, '0');
        return y + '-' + mo + '-' + dd + ' ' + h + ':' + m + ':' + s;
    }

    function formatShortTime(dateStr) {
        if (!dateStr) return null;
        var d = new Date(dateStr.replace(/\//g, '-'));
        if (isNaN(d.getTime())) return dateStr;
        return _formatDatetime(d);
    }

    function formatFullTime(d) {
        if (!d || isNaN(d.getTime())) return '';
        return _formatDatetime(d);
    }

    return {
        relative: formatRelativeTime,
        short: formatShortTime,
        full: formatFullTime
    };
})();
