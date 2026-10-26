#!/usr/bin/env python3
"""Black-box executable smoke test with an isolated temporary SQLite database."""
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / 'backend/roadmap'

def main():
    with tempfile.TemporaryDirectory(prefix='roadmap-smoke-') as directory:
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        password = secrets.token_urlsafe(24)
        database = Path(directory) / 'roadmap.db'
        env = {**os.environ, 'ROADMAP_ADDR': f'127.0.0.1:{port}',
               'ROADMAP_PASSWORD': password, 'ROADMAP_DB': str(database),
               'ROADMAP_COOKIE_SECURE': 'false', 'ROADMAP_DEMO': 'false',
               'ROADMAP_STATIC': str(ROOT / 'frontend/dist')}
        client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        csrf = ''

        def request(path, method='GET', body=None, expected=200):
            headers = {'Content-Type': 'application/json', 'X-CSRF-Token': csrf}
            data = None if body is None else json.dumps(body).encode()
            req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
            try:
                response = client.open(req, timeout=5)
            except urllib.error.HTTPError as exc:
                response = exc
            assert response.status == expected, (path, response.status, response.read())
            payload = response.read()
            return json.loads(payload) if payload else None

        def start():
            process = subprocess.Popen([str(BINARY)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            for _ in range(100):
                if process.poll() is not None:
                    raise RuntimeError('Server exited before readiness')
                try:
                    request('/healthz')
                    return process
                except urllib.error.URLError:
                    time.sleep(.05)
            process.terminate()
            process.wait(timeout=10)
            raise RuntimeError('Server readiness timeout')

        process = start()
        try:
            request('/api/graph', expected=401)
            csrf = request('/api/auth/login', 'POST', {'password': password})['csrf_token']
            project = request('/api/projects', 'POST', {'name': 'Тест платформы', 'slug': 'smoke', 'color': '#5588ff'}, 201)
            ids = []
            for title, date in [('Prerequisite', '2027-03-20'), ('Dependent', '2027-03-15')]:
                ids.append(request('/api/milestones', 'POST', {'project_id': project['id'], 'title': title,
                    'planned_date': date, 'milestone_type': 'release', 'status': 'planned'}, 201)['id'])
            request('/api/dependencies', 'POST', {'prerequisite_milestone_id': ids[0],
                'dependent_milestone_id': ids[1], 'dependency_type': 'requires'}, 201)
            assert request('/api/analysis/conflicts')[0]['days'] == 5
            assert request('/api/milestones/' + ids[0] + '/impact')['direct'] == [ids[1]]
            request('/api/dependencies', 'POST', {'prerequisite_milestone_id': ids[1],
                'dependent_milestone_id': ids[0], 'dependency_type': 'requires'}, 422)
            before = request('/api/graph')
        finally:
            process.terminate()
            process.wait(timeout=10)
        process = start()
        try:
            request('/api/graph', expected=401)
            csrf = request('/api/auth/login', 'POST', {'password': password})['csrf_token']
            assert request('/api/graph') == before
            assert database.stat().st_mode & 0o777 == 0o600
            print('PASS: executable auth, Unicode CRUD, direction, conflicts, impact, cycle rejection, restart persistence, DB permissions')
        finally:
            process.terminate()
            process.wait(timeout=10)

if __name__ == '__main__':
    main()
