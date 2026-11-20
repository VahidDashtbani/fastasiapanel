(function () {
  'use strict';

  var root = document.documentElement;
  try {
    root.dataset.theme = localStorage.getItem('fap-theme') || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  } catch (e) {}

  var form = document.getElementById('loginForm');
  var err = document.getElementById('loginErr');

  form.addEventListener('submit', function (e) {
    e.preventDefault();
    err.textContent = '';
    var f = form.elements;
    fetch('api/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: f.username.value.trim(), password: f.password.value })
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) throw new Error(data.error || 'Invalid username or password');
        location.href = './';
      });
    }).catch(function (x) {
      err.textContent = x.message;
    });
  });
})();