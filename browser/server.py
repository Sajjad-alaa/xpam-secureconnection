#!/usr/bin/env python3
"""
XPAM browser service — web assets in any browser, without showing the site address.

Every web session gets its own Chromium on its own X display, in kiosk mode (no address bar, no
tabs, no developer tools), shown through VNC by Lion (the remote desktop service), so it works the
same in the web workspace and in the desktop client and is recorded like any remote session.

- Auto-login: the asset's selectors (or script) are filled in through the Chrome DevTools Protocol
  inside this container; the password never reaches the user's computer. The site is covered by a
  "Signing in" screen meanwhile. Handled: one- and two-step login forms (also in same-origin frames
  and shadow DOM), HTTP authentication, MFA/captcha steps (handed to the user), wrong password,
  locked account, password change required, unreachable site, and being signed out later (signed
  in again). When the sign-in fails the user is told why and the session ends.
- Navigation: top-level pages may only be on the asset's own site and its "Allowed sites"; other
  pages are blocked (the page itself may still load images, scripts and APIs it needs).
- Profiles live in a temporary folder that is deleted when the session ends.

API (only Lion calls it; Authorization: Bearer <BROWSER_KEY>):
  POST /sessions               {url, width, height, username, password, autofill, username_selector,
                                password_selector, submit_selector, script, allowed_urls, ignore_cert}
                               -> {container_id, host, port, protocol: "vnc", username, password}
  POST /sessions/<id>/release  end the session
  GET  /health                 {"ok": true, "sessions": n}
"""
import base64
import hmac
import json
import os
import secrets
import shutil
import signal
import socket
import struct
import subprocess
import threading
import time
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

KEY = os.environ.get('BROWSER_KEY', '')
ADVERTISE = os.environ.get('BROWSER_ADVERTISE_HOST') or socket.gethostname()
MAX_SESSIONS = int(os.environ.get('BROWSER_MAX_SESSIONS', '10'))
MAX_HOURS = float(os.environ.get('BROWSER_MAX_HOURS', '12'))
ROOT = '/tmp/xpam-browser'
FIRST_DISPLAY = 20

sessions = {}
lock = threading.Lock()


def log(*args):
    print(time.strftime('%Y-%m-%d %H:%M:%S'), *args, flush=True)


# ---------------------------------------------------------------- minimal Chrome DevTools client

class CDP:
    """Just enough of a WebSocket client to talk to one Chromium page target."""

    def __init__(self, ws_url, timeout=10):
        u = urllib.parse.urlparse(ws_url)
        self.sock = socket.create_connection((u.hostname, u.port), timeout=timeout)
        key = base64.b64encode(os.urandom(16)).decode()
        req = (f'GET {u.path} HTTP/1.1\r\nHost: {u.hostname}:{u.port}\r\nUpgrade: websocket\r\n'
               f'Connection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n')
        self.sock.sendall(req.encode())
        resp = b''
        while b'\r\n\r\n' not in resp:
            chunk = self.sock.recv(4096)
            if not chunk:
                raise ConnectionError('CDP handshake failed')
            resp += chunk
        if b' 101 ' not in resp.split(b'\r\n')[0]:
            raise ConnectionError('CDP handshake refused')
        self.buf = resp.split(b'\r\n\r\n', 1)[1]
        self.next_id = 0
        self.events = []
        self.replies = {}
        self.wlock = threading.Lock()

    def _send(self, data):
        payload = data.encode()
        header = bytearray([0x81])
        n = len(payload)
        if n < 126:
            header.append(0x80 | n)
        elif n < 65536:
            header += bytes([0x80 | 126]) + struct.pack('!H', n)
        else:
            header += bytes([0x80 | 127]) + struct.pack('!Q', n)
        mask = os.urandom(4)
        masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
        with self.wlock:
            self.sock.sendall(bytes(header) + mask + masked)

    def _recv_exact(self, n):
        while len(self.buf) < n:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise ConnectionError('CDP closed')
            self.buf += chunk
        data, self.buf = self.buf[:n], self.buf[n:]
        return data

    def recv(self):
        message = b''
        while True:
            b1, b2 = self._recv_exact(2)
            fin, opcode, n = b1 & 0x80, b1 & 0x0F, b2 & 0x7F
            if n == 126:
                n = struct.unpack('!H', self._recv_exact(2))[0]
            elif n == 127:
                n = struct.unpack('!Q', self._recv_exact(8))[0]
            data = self._recv_exact(n)
            if opcode == 8:
                raise ConnectionError('CDP closed')
            if opcode == 9:  # ping
                continue
            message += data
            if fin:
                return json.loads(message.decode('utf-8', 'replace'))

    def call(self, method, params=None, timeout=15, session_id=None):
        self.next_id += 1
        mid = self.next_id
        msg = {'id': mid, 'method': method, 'params': params or {}}
        if session_id:
            msg['sessionId'] = session_id
        self._send(json.dumps(msg))
        end = time.time() + timeout
        while time.time() < end:
            # an answer may already have been read by a nested call (event handlers call too)
            reply = self.replies.pop(mid, None) or self.recv()
            if reply.get('id') == mid:
                if 'error' in reply:
                    raise RuntimeError(reply['error'].get('message', 'CDP error'))
                return reply.get('result', {})
            if 'method' in reply:
                self.on_event(reply)
            elif reply.get('id'):
                self.replies[reply['id']] = reply
        raise TimeoutError(method)

    def pump(self, seconds):
        """Handle events (paused navigations) for a while; frames are only read when data is waiting."""
        import select
        end = time.time() + seconds
        while True:
            remaining = end - time.time()
            if remaining <= 0:
                break
            if not self.buf:
                ready, _, _ = select.select([self.sock], [], [], remaining)
                if not ready:
                    break
            msg = self.recv()
            if 'method' in msg:
                self.on_event(msg)

    def notify(self, method, params, session_id):
        """Send without waiting for the answer."""
        self.next_id += 1
        self._send(json.dumps({'id': self.next_id, 'method': method, 'params': params, 'sessionId': session_id}))

    def on_event(self, msg):
        self.events.append(msg)

    def close(self):
        try:
            self.sock.close()
        except OSError:
            pass


# ---------------------------------------------------------------- page helpers and auto-login

# Runs inside the page. Finds fields in the page, its same-origin frames and open shadow roots, and
# reports what kind of page is shown (login form, verification step, error, success...).
XP_JS = r"""const XP = (() => {
  const KINDS = ['id', 'name', 'class_name', 'type', 'xpath', 'css', 'css_selector', 'tag_name', 'link_text'];
  const docs = () => {
    const out = [];
    const walk = d => {
      out.push(d);
      for (const f of d.querySelectorAll('iframe,frame')) {
        try { if (f.contentDocument) walk(f.contentDocument); } catch (e) { /* another origin */ }
      }
    };
    walk(document);
    return out;
  };
  const deep = (root, css, out) => {
    try { out.push(...root.querySelectorAll(css)); } catch (e) { return out; }
    for (const h of root.querySelectorAll('*')) if (h.shadowRoot) deep(h.shadowRoot, css, out);
    return out;
  };
  const all = css => { const out = []; for (const d of docs()) deep(d, css, out); return out; };
  const vis = el => {
    if (!el || !el.isConnected || el.disabled || !el.getClientRects().length) return false;
    const s = el.ownerDocument.defaultView.getComputedStyle(el);
    return s.visibility !== 'hidden' && s.display !== 'none' && parseFloat(s.opacity || '1') > 0.05;
  };
  const q = s => JSON.stringify(s);
  const find = sel => {
    sel = (sel || '').trim();
    if (!sel) return [];
    const i = sel.indexOf('=');
    const kind = i > 0 ? sel.slice(0, i).trim().toLowerCase() : '';
    const known = KINDS.includes(kind);
    const v = known ? sel.slice(i + 1).trim() : sel;
    switch (known ? kind : 'css') {
      case 'id': return all('[id=' + q(v) + ']');
      case 'name': return all('[name=' + q(v) + ']');
      case 'class_name': return all('.' + CSS.escape(v));
      case 'type': return all('input[type=' + q(v) + ']');
      case 'link_text': return all('a,button').filter(e => (e.innerText || '').trim() === v);
      case 'xpath': return docs().map(d => {
        try { return d.evaluate(v, d, null, 9, null).singleNodeValue; } catch (e) { return null; }
      }).filter(Boolean);
      default: return all(v);
    }
  };
  const pick = sel => find(sel).find(vis) || null;
  const any = sel => pick(sel) || find(sel)[0] || null;
  const passwords = () => all('input[type=password]').filter(vis);
  const pwField = cfg => (cfg.password_selector && pick(cfg.password_selector)) || passwords()[0] || null;
  const USERISH = /^(user(_?name)?|login(_?name)?|e-?mail|identifier|loginfmt|j_username|account(_?name)?|uid|user_?id|user_login|session\[login\]|log)$/i;
  const texty = e => e.tagName === 'INPUT' && /^(text|email|tel|)$/i.test(e.type) && vis(e);
  const userField = (cfg, pw) => {
    if (cfg.username_selector) { const e = pick(cfg.username_selector); if (e) return e; }
    if (pw) {
      const scope = pw.form ? [...pw.form.elements] : all('input');
      const before = scope.filter(e => texty(e) && (e.compareDocumentPosition(pw) & 4));
      return before.length ? before[before.length - 1] : null;
    }
    return all('input').find(e => texty(e) && (e.type === 'email' || /username|email/i.test(e.autocomplete || '') ||
      USERISH.test(e.name || '') || USERISH.test(e.id || ''))) || null;
  };
  const setValue = (el, value) => {
    const w = el.ownerDocument.defaultView;
    el.focus();
    const proto = el.tagName === 'TEXTAREA' ? w.HTMLTextAreaElement.prototype : w.HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, value);
    for (const t of ['input', 'change']) el.dispatchEvent(new w.Event(t, {bubbles: true}));
    return true;
  };
  const WORDS = /^(log ?in|sign ?in|log ?on|sign ?on|submit|continue|next|enter|ok|connect|anmelden|connexion|se connecter|entrar|acceder|iniciar|accedi|войти|登录|ログイン)/i;
  const NOT_SUBMIT = /with |google|microsoft|apple|facebook|github|sso|forgot|reset|register|sign ?up|create/i;
  const press = (cfg, field) => {
    if (cfg.submit_selector) { const b = pick(cfg.submit_selector); if (b) { b.click(); return 'selector'; } }
    const form = field && field.form;
    if (form) form.noValidate = true;  // the website checks the stored account, not the browser
    let b = null;
    if (form) {
      b = [...form.querySelectorAll('button[type=submit],input[type=submit],input[type=image]')].find(vis) ||
          [...form.querySelectorAll('button:not([type])')].find(e => vis(e) && WORDS.test((e.innerText || '').trim()));
    }
    if (!b) {
      b = all('button,input[type=submit],input[type=button],[role=button],a').filter(vis).find(e => {
        const t = (e.innerText || e.value || '').trim();
        return WORDS.test(t) && !NOT_SUBMIT.test(t);
      });
    }
    if (b) { b.click(); return 'button'; }
    if (form) { form.requestSubmit ? form.requestSubmit() : form.submit(); return 'form'; }
    if (field) {
      const w = field.ownerDocument.defaultView;
      for (const t of ['keydown', 'keypress', 'keyup']) {
        field.dispatchEvent(new w.KeyboardEvent(t, {key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true}));
      }
      return 'enter';
    }
    return '';
  };
  const challenge = cfg => {
    if (cfg.interactive_selector && pick(cfg.interactive_selector)) return 'configured';
    if (all('iframe[src*="recaptcha"],iframe[src*="hcaptcha"],iframe[src*="turnstile"],iframe[src*="captcha" i],' +
            'iframe[title*="captcha" i],input[name*="captcha" i],input[id*="captcha" i],img[src*="captcha" i],' +
            'img[id*="captcha" i],img[alt*="captcha" i]').some(vis)) return 'captcha';
    if (all('input[autocomplete="one-time-code"],input[name*="otp" i],input[id*="otp" i],input[name*="totp" i],' +
            'input[name*="mfa" i],input[name*="2fa" i],input[name*="passcode" i],input[name="code"],input[id="code"],' +
            'input[name*="verification" i]').some(vis)) return 'code';
    return '';
  };
  const text = () => docs().map(d => { try { return d.body ? d.body.innerText : ''; } catch (e) { return ''; } })
    .join('\n').slice(0, 30000);
  const LOCKED = /\b(account|user)\b[^.\n]{0,40}\b(locked|disabled|suspended|blocked|deactivated|expired)\b|too many (failed |unsuccessful |invalid )?(login |sign-?in |log-?in )?attempts|temporarily (locked|blocked)/i;
  const ERROR = /\b(invalid|incorrect|wrong|failed|failure|denied|unauthori[sz]ed|not recognized|does ?n[o']t match|try again|unknown user|bad credentials)\b/i;
  return {
    state(cfg) {
      const pw = pwField(cfg);
      const t = text();
      return {
        ready: document.readyState, path: location.pathname, pw: !!pw, pwCount: passwords().length,
        user: !!userField(cfg, pw), success: !!(cfg.success_selector && pick(cfg.success_selector)),
        challenge: challenge(cfg), locked: LOCKED.test(t), error: ERROR.test(t)
      };
    },
    fill(cfg, username, password) {
      const pw = pwField(cfg);
      if (!pw) return false;
      const u = userField(cfg, pw);
      if (u && username) setValue(u, username);
      return setValue(pw, password);
    },
    submit: cfg => press(cfg, pwField(cfg)),
    fillUser(cfg, username) {
      const u = userField(cfg, null);
      if (!u) return '';
      setValue(u, username);
      return press(cfg, u);
    },
    exists: sel => !!any(sel),
    type: (sel, value) => { const e = any(sel); return e ? setValue(e, value) : false; },
    click: sel => { const e = any(sel); if (!e) return false; e.click(); return true; },
  };
})()"""


def evaluate(cdp, expression, sid):
    res = cdp.call('Runtime.evaluate', {'expression': expression, 'returnByValue': True}, session_id=sid)
    return (res.get('result') or {}).get('value')


def run(cdp, expression, sid):
    """Evaluates an XP.* expression in the page."""
    return evaluate(cdp, f'(() => {{ {XP_JS}; return {expression}; }})()', sid)


def page_state(session, cdp, sid):
    try:
        return run(cdp, f'XP.state({session.selectors})', sid) or {}
    except (TimeoutError, RuntimeError):
        return {}  # the page is changing


# Covers the site while XPAM signs in: the user never sees (or types into) the login form. Real key
# presses are swallowed; XPAM's own typing and clicks are scripted, so they still work.
SIGNIN_OVERLAY_JS = r"""(() => {
  if (window.top !== window || window.__xpamOverlay) return;
  window.__xpamOverlay = true;
  const add = () => {
    if (window.__xpamDone || !document.documentElement || document.getElementById('__xpam_signin')) return;
    const d = document.createElement('div');
    d.id = '__xpam_signin';
    d.style.cssText = 'position:fixed;inset:0;z-index:2147483647;background:#1e1e1e;color:#ddd;display:flex;' +
      'align-items:center;justify-content:center;font:18px sans-serif;cursor:progress';
    d.textContent = 'Signing in…';
    document.documentElement.appendChild(d);
  };
  new MutationObserver(add).observe(document, {childList: true, subtree: true});
  for (const t of ['keydown', 'keypress', 'keyup', 'paste']) {
    window.addEventListener(t, e => {
      if (e.isTrusted && !window.__xpamDone) { e.preventDefault(); e.stopImmediatePropagation(); }
    }, true);
  }
  add();
})()"""


def show_overlay(session, cdp, sid):
    try:
        res = cdp.call('Page.addScriptToEvaluateOnNewDocument', {'source': SIGNIN_OVERLAY_JS}, session_id=sid)
        session.overlay = res.get('identifier')
        # the page shown now (signing in again after the site signed the user out)
        evaluate(cdp, 'window.__xpamDone = false; window.__xpamOverlay = false; ' + SIGNIN_OVERLAY_JS, sid)
    except Exception as e:
        log(session.id, 'sign-in screen not available:', e.__class__.__name__)


def hide_overlay(session, cdp, sid, keep_screen=False):
    if not session.overlay:
        return
    try:
        cdp.call('Page.removeScriptToEvaluateOnNewDocument', {'identifier': session.overlay}, session_id=sid)
        if not keep_screen:  # (kept when the page is replaced next anyway)
            evaluate(cdp, "window.__xpamDone = true; document.getElementById('__xpam_signin')?.remove()", sid)
    except Exception:
        pass
    session.overlay = None


def message_html(title, lines):
    body = ''.join(f'<p>{line}</p>' for line in lines)
    return ('<html><head><title>' + title + '</title></head><body style="font-family:sans-serif;background:#1e1e1e;'
            'color:#ddd;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">'
            '<div style="text-align:center;max-width:560px"><h2>' + title + '</h2>' + body + '</div></body></html>')


CLOSING = 'This session closes in a few seconds.'
FAILURES = {
    'rejected': ('Automatic sign-in failed', 'The website did not accept the account stored in XPAM '
                 '(the password may have changed).', 'Ask your administrator to update the account.'),
    'locked': ('Account locked', 'The website says this account is locked or disabled.',
               'Ask your administrator to unlock it.'),
    'password_change': ('New password required', 'The website asks for a new password for this account.',
                        'Ask your administrator to change the password in XPAM.'),
    'unreachable': ('Website not reachable', 'The website did not respond.',
                    'Try again later, or ask your administrator to check the asset.'),
    'timeout_user': ('Verification not completed', 'The verification step (code or captcha) was not completed '
                     'in time.', 'Open the asset again to retry.'),
    'signed_out': ('Signed out', 'The website keeps signing this account out.',
                   'Open the asset again, or ask your administrator.'),
}


def failure_url(reason):
    title, *lines = FAILURES.get(reason) or FAILURES['rejected']
    return 'data:text/html;base64,' + base64.b64encode(message_html(title, lines + [CLOSING]).encode()).decode()


# shown instead of Chromium's error page, which would show the site address
NET_ERROR_HTML = message_html('Page not available', [
    'The website did not respond.',
    '<button onclick="location.reload()" style="font-size:15px;padding:6px 18px;cursor:pointer">Try again</button>'])


# ---------------------------------------------------------------- sessions

def host_allowed(session, url):
    try:
        u = urllib.parse.urlparse(url)
    except ValueError:
        return False
    if u.scheme in ('about', 'data', 'blob'):
        return True
    if u.scheme not in ('http', 'https'):
        return False
    host = (u.hostname or '').lower()
    for allowed in session.allowed:
        if allowed.startswith('*.') and (host == allowed[2:] or host.endswith(allowed[1:])):
            return True
        if host == allowed:
            return True
    return False


BLOCKED_PAGE = base64.b64encode(
    b'<html><body style="font-family:sans-serif;background:#1e1e1e;color:#ddd;display:flex;'
    b'align-items:center;justify-content:center;height:100vh;margin:0"><div style="text-align:center">'
    b'<h2>Blocked by XPAM</h2><p>This page is outside the sites allowed for this asset.</p>'
    b'<p><a style="color:#38bdf8" href="#" onclick="history.back();return false">Back</a></p></div></body></html>').decode()


class Session:
    def __init__(self, sid, display, config):
        self.id = sid
        self.display = display
        self.vnc_port = 5900 + display
        self.cdp_port = 9300 + display
        self.password = secrets.token_urlsafe(12)[:8]  # VNC passwords are 8 characters
        self.config = config
        self.selectors = json.dumps({k: config.get(k) or '' for k in (
            'username_selector', 'password_selector', 'submit_selector', 'success_selector', 'interactive_selector')})
        self.dir = os.path.join(ROOT, sid)
        self.procs = []
        self.created = time.time()
        self.closed = threading.Event()
        self.attached = {}
        self.main_sid = self.main_target = None
        self.overlay = None
        self.signing_in = False
        self.unreachable = False
        self.auth_given, self.auth_failed = set(), False
        self.doc_status, self.doc_time = None, 0.0
        self.login_path = None
        self.two_step = False
        self.relogins = []
        self.check_login_at, self.check_again = None, False
        url = config['url']
        host = (urllib.parse.urlparse(url).hostname or '').lower()
        self.allowed = {host}
        for item in config.get('allowed_urls') or []:
            item = str(item).strip().lower()
            if not item:
                continue
            parsed = urllib.parse.urlparse(item if '://' in item else f'http://{item}')
            if parsed.hostname:
                self.allowed.add(('*.' if item.startswith('*.') else '') + parsed.hostname.lstrip('*.'))

    def start(self):
        os.makedirs(self.dir, exist_ok=True)
        w, h = self.config['width'], self.config['height']
        env = dict(os.environ, DISPLAY=f':{self.display}', HOME=self.dir)
        self.procs.append(subprocess.Popen(
            ['Xvfb', f':{self.display}', '-screen', '0', f'{w}x{h}x24', '-nolisten', 'tcp', '-dpi', '96'],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
        for _ in range(50):
            if os.path.exists(f'/tmp/.X11-unix/X{self.display}'):
                break
            time.sleep(0.1)
        pwfile = os.path.join(self.dir, '.vncpass')
        subprocess.run(['x11vnc', '-storepasswd', self.password, pwfile], stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL, check=True)
        self.procs.append(subprocess.Popen(
            ['x11vnc', '-display', f':{self.display}', '-rfbport', str(self.vnc_port), '-rfbauth', pwfile,
             '-forever', '-shared', '-noxdamage', '-quiet', '-nopw', '-xkb'],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, env=env))
        args = [
            'chromium', '--kiosk', 'about:blank',  # the guard opens the site once it is watching
            f'--user-data-dir={self.dir}/profile', f'--remote-debugging-port={self.cdp_port}',
            '--remote-debugging-address=127.0.0.1', f'--window-size={w},{h}', '--window-position=0,0',
            '--no-first-run', '--no-default-browser-check', '--disable-translate', '--disable-sync',
            '--disable-features=Translate,PasswordManagerOnboarding,MediaRouter,OptimizationHints',
            '--password-store=basic', '--disable-dev-shm-usage', '--no-sandbox', '--test-type',
            '--disable-gpu', '--disable-background-networking', '--lang=en-US', '--noerrdialogs',
            '--disable-session-crashed-bubble', '--hide-crash-restore-bubble',
        ]
        if self.config.get('ignore_cert'):
            args.append('--ignore-certificate-errors')
        self.procs.append(subprocess.Popen(args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, env=env))
        threading.Thread(target=self.guard, daemon=True).start()

    def browser_ws(self):
        for _ in range(100):
            try:
                with urllib.request.urlopen(f'http://127.0.0.1:{self.cdp_port}/json/version', timeout=2) as r:
                    return json.load(r)['webSocketDebuggerUrl']
            except Exception:
                time.sleep(0.2)
        return None

    def can_sign_in(self):
        return (self.config.get('autofill') or 'basic') != 'no' and bool(self.config.get('password'))

    def guard(self):
        """One DevTools connection: sign-in on the first page and keep every page on the allowed sites."""
        ws = self.browser_ws()
        if not ws:
            log(self.id, 'browser did not start')
            return
        try:
            cdp = CDP(ws)
        except Exception as e:
            log(self.id, 'cannot reach the browser:', e)
            return
        cdp.on_event = lambda msg: self.on_event(cdp, msg)
        deadline = self.created + MAX_HOURS * 3600
        try:
            cdp.call('Target.setDiscoverTargets', {'discover': True})
            for info in cdp.call('Target.getTargets').get('targetInfos', []):
                self.attach(cdp, info)
            # pages may already be attached through the "target created" events
            end = time.time() + 10
            while not self.attached and time.time() < end:
                cdp.pump(0.2)
            if not self.attached:
                log(self.id, 'no browser page')
                return
            self.main_target, self.main_sid = next(iter(self.attached.items()))
            sid = self.main_sid
            signs_in = self.can_sign_in()
            if signs_in:
                show_overlay(self, cdp, sid)
            if not self.open_site(cdp, sid):
                log(self.id, 'website not reachable: closing')
                return self.finish(cdp, sid, 'unreachable')
            if signs_in:
                result, reason = self.sign_in(cdp, sid)
                if result == 'failed':
                    return self.finish(cdp, sid, reason)
            while not self.closed.is_set():
                if time.time() > deadline:
                    log(self.id, 'maximum session time reached')
                    threading.Thread(target=release, args=(self.id,), daemon=True).start()
                    return
                if self.check_login_at and time.time() >= self.check_login_at:
                    self.check_login_at = None
                    result, reason = self.check_signed_out(cdp, sid)
                    if result is None and self.check_again:
                        self.check_login_at, self.check_again = time.time() + 2, False
                    if result == 'failed':
                        return self.finish(cdp, sid, reason)
                cdp.pump(0.25)
        except ConnectionError:
            pass
        except Exception as e:
            log(self.id, 'browser guard stopped:', e.__class__.__name__, e)
        finally:
            cdp.close()

    def open_site(self, cdp, sid):
        """Opens the asset's address; False when the site cannot be reached (three tries)."""
        for attempt in range(3):
            self.unreachable = False
            try:
                res = cdp.call('Page.navigate', {'url': self.config['url']}, timeout=40, session_id=sid)
            except TimeoutError:
                return True  # still loading: the sign-in waits for the page
            error = res.get('errorText') or ''
            if not error or error == 'net::ERR_ABORTED':
                return True
            log(self.id, 'website not reachable:', error)
            if attempt < 2:
                cdp.pump(3)
        return False

    def finish(self, cdp, sid, reason):
        """Tells the user why and ends the session: the login page is never left to the user."""
        hide_overlay(self, cdp, sid, keep_screen=True)
        try:
            cdp.call('Page.navigate', {'url': failure_url(reason)}, session_id=sid)
        except Exception:
            pass
        cdp.pump(8)
        threading.Thread(target=release, args=(self.id,), daemon=True).start()

    # ------------------------------------------------------------ sign-in

    def sign_in(self, cdp, sid):
        """Returns ('ok' | 'skipped' | 'failed', reason). The caller covers the page first (show_overlay)."""
        self.signing_in = True
        try:
            cdp.call('Network.enable', {}, session_id=sid)  # status of the page after the form is sent
        except Exception:
            pass
        try:
            if (self.config.get('autofill') or 'basic') == 'script' and self.config.get('script'):
                result = self._script(cdp, sid)
            else:
                result = self._basic(cdp, sid)
        except ConnectionError:
            raise
        except Exception as e:
            log(self.id, 'auto-login error:', e.__class__.__name__, e)
            result = ('skipped', '')
        finally:
            self.signing_in = False
            try:
                cdp.call('Network.disable', {}, session_id=sid)
            except Exception:
                pass
        hide_overlay(self, cdp, sid, keep_screen=result[0] == 'failed')
        if result[0] == 'ok':
            log(self.id, 'auto-login done' + (' (HTTP authentication)' if result[1] == 'http' else ''))
        elif result[0] == 'skipped':
            log(self.id, 'auto-login: no sign-in page found, showing the site')
        else:
            log(self.id, f'auto-login FAILED ({result[1]})')
        return result

    def _http_signed_in(self):
        return bool(self.auth_given) and not self.auth_failed

    def _basic(self, cdp, sid):
        cfg, sel = self.config, self.selectors
        end, ready_since, user_step_done = time.time() + 45, None, False
        self.two_step = False
        while time.time() < end:
            if self.unreachable:
                return 'failed', 'unreachable'
            if self.auth_failed:
                return 'failed', 'rejected'
            st = page_state(self, cdp, sid)
            if st.get('pw'):
                self.login_path = st.get('path')
                run(cdp, f'XP.fill({sel}, {json.dumps(cfg["username"])}, {json.dumps(cfg["password"])})', sid)
                if st.get('challenge') in ('captcha', 'configured'):
                    # a captcha on the login form: the account is filled in, the user solves it and signs in
                    hide_overlay(self, cdp, sid)
                    log(self.id, 'auto-login: waiting for the user to solve the captcha')
                    return self._verify(cdp, sid, st, user_step=True)
                cdp.pump(0.2)
                self.doc_status = None
                run(cdp, f'XP.submit({sel})', sid)
                return self._verify(cdp, sid, st)
            if st.get('user') and cfg.get('username') and not user_step_done and st.get('ready') == 'complete':
                # two-step sign-in: the user name first, the password on the next page
                user_step_done, ready_since, self.two_step = True, None, True
                run(cdp, f'XP.fillUser({sel}, {json.dumps(cfg["username"])})', sid)
                cdp.pump(1.5)
                continue
            if st.get('ready') == 'complete':
                ready_since = ready_since or time.time()
                waited = time.time() - ready_since
                if (self._http_signed_in() and waited > 1.5) or waited > 6:
                    break
            else:
                ready_since = None
            cdp.pump(0.5)
        return ('ok', 'http') if self._http_signed_in() else ('skipped', '')

    def _wait_target(self, cdp, sid, target, timeout):
        end = time.time() + timeout
        while time.time() < end and not self.unreachable:
            try:
                if run(cdp, f'XP.exists({json.dumps(target)})', sid):
                    return True
            except (TimeoutError, RuntimeError):
                pass
            cdp.pump(0.5)
        return False

    def _script(self, cdp, sid):
        cfg = self.config
        steps = sorted([s for s in cfg['script'] if isinstance(s, dict)], key=lambda s: s.get('step', 0))
        typed_secret, before = False, {}
        for step in steps[:128]:
            if self.unreachable:
                return 'failed', 'unreachable'
            if self.auth_failed:
                return 'failed', 'rejected'
            cmd, target, raw = step.get('command'), str(step.get('target') or ''), str(step.get('value') or '')
            value = raw.replace('{USERNAME}', cfg['username']).replace('{SECRET}', cfg['password'])
            timeout = min(int(step.get('timeout') or 15), 60)
            if cmd == 'sleep':
                cdp.pump(min(int(target or 0), 30))
            elif cmd == 'open':
                cdp.call('Page.navigate', {'url': value or target}, session_id=sid)
                cdp.pump(2)
            elif cmd in ('type', 'click', 'button', 'check'):
                if not self._wait_target(cdp, sid, target, timeout):
                    log(self.id, f'auto-login script: step {step.get("step")} not found on the page')
                    continue
                if cmd == 'type':
                    if '{SECRET}' in raw:
                        before = page_state(self, cdp, sid)
                        self.login_path, typed_secret = before.get('path'), True
                    run(cdp, f'XP.type({json.dumps(target)}, {json.dumps(value)})', sid)
                else:
                    self.doc_status = None
                    run(cdp, f'XP.click({json.dumps(target)})', sid)
        if not typed_secret:
            return ('ok', 'http') if self._http_signed_in() else ('skipped', '')
        return self._verify(cdp, sid, before)

    def _verify(self, cdp, sid, before, user_step=False):
        """
        Decides whether the website accepted the account after the form was sent:
        - ok: the success selector is shown, or the login form is gone (and no 401/403);
        - a verification step (MFA code, captcha) is handed to the user for up to three minutes;
        - failed: the login form is back (new page, 401/403 or an error message), the account is
          locked, or the site asks for a new password.
        """
        success = bool(self.config.get('success_selector'))
        cdp.pump(1)
        start, waiting, gone, st = time.time(), user_step, None, {}
        end = start + (180 if waiting else 20 if success else 10)
        while time.time() < end:
            if self.unreachable:
                return 'failed', 'unreachable'
            if self.auth_failed:
                return 'failed', 'rejected'
            st = page_state(self, cdp, sid)
            if not st:
                cdp.pump(0.5)
                continue
            if st.get('success'):
                return 'ok', ''
            if st.get('locked') and not before.get('locked'):
                return 'failed', 'locked'
            challenge = st.get('challenge')
            if st.get('pwCount', 0) >= 2 and not challenge:
                return 'failed', 'password_change'
            if challenge and not waiting:
                waiting, end = True, time.time() + 180
                hide_overlay(self, cdp, sid)  # the user completes this step
                log(self.id, 'auto-login: waiting for the user to finish the verification')
            # two-step sign-in sent back to its first page: the login form is back as well
            form_back = st.get('pw') or (self.two_step and st.get('user'))
            if form_back:
                gone = None
                new_page = self.doc_status is not None and time.time() - self.doc_time > 1.5 and st.get('ready') == 'complete'
                if not waiting and (self.doc_status in (401, 403) or new_page or
                                    (st.get('error') and not before.get('error'))):
                    return 'failed', 'rejected'
            elif not challenge and st.get('ready') == 'complete':  # (not while the next page is loading)
                if self.doc_status in (401, 403):
                    return 'failed', 'rejected'
                gone = gone or time.time()
                if time.time() - gone >= (5 if success else 0.5):
                    if success:
                        log(self.id, 'auto-login: the success selector is not on the page, but the form is gone')
                    return 'ok', ''
            cdp.pump(0.5)
        if st and not st.get('pw') and not st.get('challenge'):
            return 'ok', ''
        return 'failed', ('timeout_user' if waiting else 'rejected')

    def check_signed_out(self, cdp, sid):
        """After a page load: is the website's login form back? Then sign in again (at most 3 times in 10 minutes)."""
        if not self.can_sign_in():
            return None, ''
        st = page_state(self, cdp, sid)
        if not (st.get('pw') and st.get('pwCount') == 1 and not st.get('success')
                and (st.get('user') or (self.login_path and st.get('path') == self.login_path))):
            return None, ''
        now = time.time()
        self.relogins = [t for t in self.relogins if now - t < 600] + [now]
        if len(self.relogins) > 3:
            log(self.id, 'the website keeps signing the account out')
            return 'failed', 'signed_out'
        log(self.id, 'the website signed the account out: signing in again')
        show_overlay(self, cdp, sid)
        return self.sign_in(cdp, sid)

    # ------------------------------------------------------------ pages and events

    def attach(self, cdp, info):
        url = str(info.get('url', ''))
        if url.startswith('devtools://') or info.get('type') in ('devtools',):
            # developer tools are only for XPAM's own connection: close any window that opens
            try:
                cdp.call('Target.closeTarget', {'targetId': info['targetId']})
                log(self.id, 'closed developer tools')
            except Exception:
                pass
            return None
        if info.get('type') != 'page' or info['targetId'] in self.attached:
            return None
        if str(info.get('url', '')).startswith(('chrome://', 'chrome-untrusted://', 'devtools://')):
            return None
        sid = cdp.call('Target.attachToTarget', {'targetId': info['targetId'], 'flatten': True})['sessionId']
        self.attached[info['targetId']] = sid
        # handleAuthRequests: HTTP authentication (Basic, Digest, NTLM) is answered here, never by a browser prompt
        cdp.call('Fetch.enable', {'patterns': [{'resourceType': 'Document', 'requestStage': 'Request'}],
                                  'handleAuthRequests': True}, session_id=sid)
        cdp.call('Page.enable', {}, session_id=sid)
        return sid

    def on_event(self, cdp, msg):
        method, params, sid = msg.get('method'), msg.get('params', {}), msg.get('sessionId')
        if method in ('Target.targetCreated', 'Target.targetInfoChanged'):
            try:
                self.attach(cdp, params.get('targetInfo', {}))
            except Exception:
                pass
        elif method == 'Fetch.requestPaused':
            if host_allowed(self, params['request']['url']):
                cdp.notify('Fetch.continueRequest', {'requestId': params['requestId']}, sid)
            else:
                log(self.id, 'blocked navigation to another site')
                cdp.notify('Fetch.fulfillRequest', {
                    'requestId': params['requestId'], 'responseCode': 403,
                    'responseHeaders': [{'name': 'Content-Type', 'value': 'text/html'}], 'body': BLOCKED_PAGE}, sid)
        elif method == 'Fetch.authRequired':
            ch = params.get('authChallenge') or {}
            key = (ch.get('origin'), ch.get('scheme'), ch.get('realm'))
            if ch.get('source') == 'Server' and self.can_sign_in() and key not in self.auth_given:
                self.auth_given.add(key)
                answer = {'response': 'ProvideCredentials', 'username': self.config['username'],
                          'password': self.config['password']}
            else:
                if key in self.auth_given:  # asked again for the same site: the account was refused
                    self.auth_failed = True
                    log(self.id, 'the website refused the HTTP sign-in')
                answer = {'response': 'CancelAuth'}
            cdp.notify('Fetch.continueWithAuth', {'requestId': params['requestId'], 'authChallengeResponse': answer}, sid)
        elif method == 'Page.frameNavigated':
            frame = params.get('frame') or {}
            if frame.get('parentId'):
                return
            url = frame.get('url', '')
            if url.startswith('chrome-error://'):
                if sid == self.main_sid:
                    self.unreachable = True
                # Chromium's error page names the site: show XPAM's own (Try again reloads the page)
                cdp.notify('Runtime.evaluate', {'expression': 'document.documentElement.innerHTML = '
                                                + json.dumps(NET_ERROR_HTML)}, sid)
            elif sid == self.main_sid and url.startswith(('http://', 'https://')) and not self.signing_in:
                self.check_login_at = time.time() + 2  # was the account signed out?
        elif method == 'Page.domContentEventFired':
            if sid == self.main_sid and not self.signing_in:
                # check at once (the login form must not stay usable), again a bit later for pages built by scripts
                self.check_login_at, self.check_again = time.time(), True
        elif method == 'Network.responseReceived':
            if sid == self.main_sid and params.get('type') == 'Document' and params.get('frameId') == self.main_target:
                self.doc_status, self.doc_time = (params.get('response') or {}).get('status'), time.time()

    def stop(self):
        self.closed.set()
        self.config['password'] = ''
        for p in reversed(self.procs):
            try:
                p.terminate()
            except Exception:
                pass
        for p in reversed(self.procs):
            try:
                p.wait(timeout=5)
            except Exception:
                try:
                    p.kill()
                except Exception:
                    pass
        shutil.rmtree(self.dir, ignore_errors=True)


def create(config):
    with lock:
        if len(sessions) >= MAX_SESSIONS:
            raise RuntimeError('The browser service is busy (maximum sessions reached)')
        used = {s.display for s in sessions.values()}
        display = next(d for d in range(FIRST_DISPLAY, FIRST_DISPLAY + 500) if d not in used)
        sid = secrets.token_hex(12)
        session = Session(sid, display, config)
        sessions[sid] = session
    try:
        session.start()
    except Exception:
        release(sid)
        raise
    # wait until VNC answers so Lion connects at once
    for _ in range(60):
        try:
            socket.create_connection(('127.0.0.1', session.vnc_port), timeout=0.5).close()
            break
        except OSError:
            time.sleep(0.2)
    log(sid, 'started for', urllib.parse.urlparse(config['url']).hostname, f'{config["width"]}x{config["height"]}')
    return session


def release(sid):
    with lock:
        session = sessions.pop(sid, None)
    if session:
        session.stop()
        log(sid, 'released')
    return bool(session)


# ---------------------------------------------------------------- HTTP API

def clean_config(data):
    url = str(data.get('url') or '').strip()
    parsed = urllib.parse.urlparse(url)
    if parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password:
        raise ValueError('invalid url')
    width = max(640, min(int(data.get('width') or 1366), 3840))
    height = max(480, min(int(data.get('height') or 768), 2160))
    keep = ('username', 'password', 'autofill', 'username_selector', 'password_selector', 'submit_selector',
            'success_selector', 'interactive_selector')
    config = {k: str(data.get(k) or '')[:4096] for k in keep}
    config.update(url=url, width=width - width % 2, height=height - height % 2,
                  script=data.get('script') if isinstance(data.get('script'), list) else [],
                  allowed_urls=[str(u) for u in (data.get('allowed_urls') or [])][:50],
                  ignore_cert=bool(data.get('ignore_cert', True)))
    return config


class Handler(BaseHTTPRequestHandler):
    server_version = 'xpam-browser'

    def log_message(self, *args):
        pass

    def reply(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def authorized(self):
        auth = self.headers.get('Authorization', '')
        return bool(KEY) and hmac.compare_digest(auth, f'Bearer {KEY}')

    def do_GET(self):
        if self.path == '/health':
            return self.reply(200, {'ok': True, 'sessions': len(sessions), 'max': MAX_SESSIONS})
        self.reply(404, {'error': 'not found'})

    def do_POST(self):
        if not self.authorized():
            return self.reply(401, {'error': 'unauthorized'})
        try:
            length = min(int(self.headers.get('Content-Length') or 0), 1 << 20)
            data = json.loads(self.rfile.read(length) or b'{}')
        except (ValueError, json.JSONDecodeError):
            return self.reply(400, {'error': 'invalid json'})
        if self.path == '/sessions':
            try:
                session = create(clean_config(data))
            except (ValueError, TypeError) as e:
                return self.reply(400, {'error': str(e)})
            except Exception as e:
                return self.reply(503, {'error': str(e)})
            return self.reply(200, {'container_id': session.id, 'host': ADVERTISE, 'port': session.vnc_port,
                                    'protocol': 'vnc', 'username': '', 'password': session.password})
        if self.path.startswith('/sessions/') and self.path.endswith('/release'):
            sid = self.path.split('/')[2]
            return self.reply(200, {'released': release(sid)})
        self.reply(404, {'error': 'not found'})


def shutdown(*_):
    for sid in list(sessions):
        release(sid)
    os._exit(0)


if __name__ == '__main__':
    import faulthandler
    faulthandler.register(signal.SIGUSR1, all_threads=True)  # docker kill -s USR1: thread stacks to the log
    if not KEY:
        log('BROWSER_KEY is not set: every request will be refused')
    shutil.rmtree(ROOT, ignore_errors=True)
    os.makedirs(ROOT, exist_ok=True)
    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)
    server = ThreadingHTTPServer(('0.0.0.0', int(os.environ.get('BROWSER_PORT', '9001'))), Handler)
    log('XPAM browser service listening on', server.server_address, 'advertising', ADVERTISE)
    server.serve_forever()
