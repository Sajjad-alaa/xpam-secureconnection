#!/usr/bin/env python3
"""
XPAM browser service — web assets in any browser, without showing the site address.

Every web session gets its own Chromium on its own X display, in kiosk mode (no address bar, no
tabs, no developer tools), shown through VNC by Lion (the remote desktop service), so it works the
same in the web workspace and in the desktop client and is recorded like any remote session.

- Auto-login: the asset's selectors (or script) are filled in through the Chrome DevTools Protocol
  inside this container; the password never reaches the user's computer.
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


# ---------------------------------------------------------------- selectors and auto-login

def js_find(selector):
    """JavaScript expression returning the element for a JumpServer-style selector (name=, id=, css=, xpath=...)."""
    kind, _, value = (selector or '').partition('=')
    kind, value = kind.strip().lower(), value.strip()
    v = json.dumps(value)
    if kind == 'id':
        return f'document.getElementById({v})'
    if kind == 'name':
        return f'document.getElementsByName({v})[0]'
    if kind == 'class_name':
        return f'document.getElementsByClassName({v})[0]'
    if kind == 'type':
        return f'document.querySelector("input[type=" + JSON.stringify({v}) + "]")'
    if kind == 'xpath':
        return f'document.evaluate({v}, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue'
    return f'document.querySelector({v})'  # css / css_selector / plain


def js_type(selector, value):
    return f'''(() => {{ const el = {js_find(selector)}; if (!el) return false; el.focus();
        const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
        Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, {json.dumps(value)});
        el.dispatchEvent(new Event('input', {{bubbles: true}})); el.dispatchEvent(new Event('change', {{bubbles: true}}));
        return true; }})()'''


def js_click(selector):
    return f'(() => {{ const el = {js_find(selector)}; if (!el) return false; el.click(); return true; }})()'


def js_exists(selector):
    return f'!!({js_find(selector)})'


def evaluate(cdp, expression, sid):
    res = cdp.call('Runtime.evaluate', {'expression': expression, 'returnByValue': True}, session_id=sid)
    return (res.get('result') or {}).get('value')


def wait_for(cdp, selector, timeout, sid):
    end = time.time() + timeout
    while time.time() < end:
        try:
            if evaluate(cdp, js_exists(selector), sid):
                return True
        except (TimeoutError, RuntimeError):
            pass
        cdp.pump(0.5)
    return False


def autofill(session, cdp, sid):
    cfg = session.config
    username, password = cfg.get('username') or '', cfg.get('password') or ''
    mode = cfg.get('autofill') or 'basic'
    if mode == 'no' or not password:
        return
    if mode == 'script' and cfg.get('script'):
        steps = sorted([s for s in cfg['script'] if isinstance(s, dict)], key=lambda s: s.get('step', 0))
        for step in steps[:128]:
            cmd, target = step.get('command'), step.get('target') or ''
            value = str(step.get('value') or '').replace('{USERNAME}', username).replace('{SECRET}', password)
            timeout = min(int(step.get('timeout') or 15), 60)
            if cmd == 'sleep':
                cdp.pump(min(int(target or 0), 30))
            elif cmd == 'type':
                if wait_for(cdp, target, timeout, sid):
                    evaluate(cdp, js_type(target, value), sid)
            elif cmd in ('click', 'button', 'check'):
                if wait_for(cdp, target, timeout, sid):
                    evaluate(cdp, js_click(target), sid)
            elif cmd == 'open':
                cdp.call('Page.navigate', {'url': value or target}, session_id=sid)
                cdp.pump(2)
        log(session.id, 'auto-login script done')
        return
    us, ps = cfg.get('username_selector') or '', cfg.get('password_selector') or ''
    sub = cfg.get('submit_selector') or ''
    if not ps:
        ps = 'css=input[type=password]'
    if not wait_for(cdp, ps, 30, sid):
        log(session.id, 'auto-login: password field not found')
        return
    if username and us and wait_for(cdp, us, 5, sid):
        evaluate(cdp, js_type(us, username), sid)
    evaluate(cdp, js_type(ps, password), sid)
    cdp.pump(0.3)
    if sub and evaluate(cdp, js_click(sub), sid):
        pass
    else:  # no submit selector: submit the form of the password field
        evaluate(cdp, f'(() => {{ const el = {js_find(ps)}; if (el && el.form) {{ el.form.requestSubmit ? '
                      f'el.form.requestSubmit() : el.form.submit(); }} }})()', sid)
    log(session.id, 'auto-login done')


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
        self.dir = os.path.join(ROOT, sid)
        self.procs = []
        self.created = time.time()
        self.closed = threading.Event()
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
            'chromium', '--kiosk', self.config['url'],
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

    def guard(self):
        """One DevTools connection: auto-login on the first page and keep every page on the allowed sites."""
        ws = self.browser_ws()
        if not ws:
            log(self.id, 'browser did not start')
            return
        try:
            cdp = CDP(ws)
        except Exception as e:
            log(self.id, 'cannot reach the browser:', e)
            return
        self.attached = {}
        cdp.on_event = lambda msg: self.on_event(cdp, msg)
        deadline = self.created + MAX_HOURS * 3600
        try:
            cdp.call('Target.setDiscoverTargets', {'discover': True})
            for info in cdp.call('Target.getTargets').get('targetInfos', []):
                self.attach(cdp, info)
            # pages may already be attached through the "target created" events
            first = next(iter(self.attached.values()), None)
            if first:
                try:
                    autofill(self, cdp, first)
                except Exception as e:
                    log(self.id, 'auto-login failed:', e.__class__.__name__)
            self.config['password'] = ''  # not needed any more
            while not self.closed.is_set():
                if time.time() > deadline:
                    log(self.id, 'maximum session time reached')
                    threading.Thread(target=release, args=(self.id,), daemon=True).start()
                    return
                cdp.pump(1)
        except ConnectionError:
            pass
        except Exception as e:
            log(self.id, 'browser guard stopped:', e.__class__.__name__, e)
        finally:
            cdp.close()

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
        cdp.call('Fetch.enable', {'patterns': [{'resourceType': 'Document', 'requestStage': 'Request'}]}, session_id=sid)
        return sid

    def on_event(self, cdp, msg):
        method, params = msg.get('method'), msg.get('params', {})
        if method in ('Target.targetCreated', 'Target.targetInfoChanged'):
            try:
                self.attach(cdp, params.get('targetInfo', {}))
            except Exception:
                pass
        elif method == 'Fetch.requestPaused':
            sid = msg.get('sessionId')
            if host_allowed(self, params['request']['url']):
                cdp.notify('Fetch.continueRequest', {'requestId': params['requestId']}, sid)
            else:
                log(self.id, 'blocked navigation to another site')
                cdp.notify('Fetch.fulfillRequest', {
                    'requestId': params['requestId'], 'responseCode': 403,
                    'responseHeaders': [{'name': 'Content-Type', 'value': 'text/html'}], 'body': BLOCKED_PAGE}, sid)

    def stop(self):
        self.closed.set()
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
    keep = ('username', 'password', 'autofill', 'username_selector', 'password_selector', 'submit_selector')
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
