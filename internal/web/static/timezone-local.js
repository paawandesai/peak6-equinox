// timezone-local.js: Client-side timezone conversion for the Local option (M4-77).
// Runs only when the tz cookie is "Local". Rewrites <time datetime> elements to
// show the browser's local timezone. Falls back to UTC when JS is disabled.

(function() {
  'use strict';

  // Read the tz cookie. Returns the value or null if not found.
  function readTzCookie() {
    var cookies = document.cookie.split('; ');
    for (var i = 0; i < cookies.length; i++) {
      var parts = cookies[i].split('=');
      if (parts[0] === 'tz') {
        return decodeURIComponent(parts[1]);
      }
    }
    return null;
  }

  // Format a Date to "YYYY-MM-DD HH:MM:SS TZ" in the browser's local timezone.
  // TZ abbreviation is not reliably available in JS, so we show the offset instead.
  function formatLocalTime(date) {
    var year = date.getFullYear();
    var month = String(date.getMonth() + 1).padStart(2, '0');
    var day = String(date.getDate()).padStart(2, '0');
    var hours = String(date.getHours()).padStart(2, '0');
    var minutes = String(date.getMinutes()).padStart(2, '0');
    var seconds = String(date.getSeconds()).padStart(2, '0');

    // Get timezone offset in the format "+05:30" or "-08:00"
    var offset = -date.getTimezoneOffset();
    var offsetSign = offset >= 0 ? '+' : '-';
    var offsetHours = String(Math.floor(Math.abs(offset) / 60)).padStart(2, '0');
    var offsetMinutes = String(Math.abs(offset) % 60).padStart(2, '0');
    var offsetStr = offsetSign + offsetHours + ':' + offsetMinutes;

    return year + '-' + month + '-' + day + ' ' + hours + ':' + minutes + ':' + seconds + ' UTC' + offsetStr;
  }

  // Rewrite all <time datetime> elements to show local time.
  function rewriteTimesToLocal() {
    var timeElements = document.querySelectorAll('time[datetime]');
    for (var i = 0; i < timeElements.length; i++) {
      var el = timeElements[i];
      var dt = el.getAttribute('datetime');
      if (!dt) continue;

      var date = new Date(dt);
      if (isNaN(date.getTime())) continue; // Invalid date

      el.textContent = formatLocalTime(date);
    }
  }

  // Run on DOM ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      if (readTzCookie() === 'Local') {
        rewriteTimesToLocal();
      }
    });
  } else {
    // DOM already loaded
    if (readTzCookie() === 'Local') {
      rewriteTimesToLocal();
    }
  }
})();
