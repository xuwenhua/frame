import json, re, urllib.request

BASE = 'http://127.0.0.1:8080/api'
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

def req(method, path, token=None, body=None):
    data = json.dumps(body).encode('utf-8') if body is not None else None
    r = urllib.request.Request(BASE + path, data=data, method=method)
    r.add_header('Content-Type', 'application/json')
    if token:
        r.add_header('Authorization', 'Bearer ' + token)
    with opener.open(r, timeout=60) as resp:
        return json.loads(resp.read().decode('utf-8'))

tok = req('POST', '/auth/login', body={'email': 'admin@frame.local', 'password': 'admin123'})['data']['token']
EP = '23249b99-0fed-47f1-9052-20b6f57eef71'

# 正确的站位句：两处主语都要带 @，否则不会被换成 <Picture N>
sit = ('@老师 坐在靠白板一侧的椅子上、身体朝向对面，@学生 坐在靠窗一侧的椅子上、'
       '身体朝向 @老师，两人隔着办公桌相对而坐、视线交汇、互相面向')

d = req('GET', '/storyboard?episodeId=%s' % EP, tok)['data']
frames = d if isinstance(d, list) else (d.get('list') or d.get('frames') or [])

done = 0
for f in frames:
    vp = f.get('videoPrompt') or ''
    if '站位：' not in vp:
        continue
    lines = [('站位：' + sit) if l.startswith('站位：') else l for l in vp.split('\n')]
    req('PUT', '/storyboard/%s' % f['id'], tok, {'videoPrompt': '\n'.join(lines)})
    done += 1

print('已修正站位行：%d 段' % done)

d2 = req('GET', '/storyboard?episodeId=%s' % EP, tok)['data']
fr2 = d2 if isinstance(d2, list) else (d2.get('list') or d2.get('frames') or [])
print('\n--- 第 1 段 ---')
print('\n'.join(fr2[0]['videoPrompt'].split('\n')[:10]))
