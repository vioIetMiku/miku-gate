#!/usr/bin/env python3
"""Verify standalone UI assets, or explicitly import the legacy embedded UI.

Only raw string literals and references to other string constants are accepted;
no Go, Node.js, network access, or evaluation of source code is required.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil

ROOT = Path(__file__).resolve().parents[1]
TOKEN = re.compile(r"\s*(?:(`[^`]*`)|([A-Za-z_][A-Za-z_0-9]*)|(\+))", re.S)


def constants(source):
    values = {}
    for path in sorted(source.glob('*.go')):
        if path.name.endswith('_test.go'):
            continue
        text = path.read_text()
        for match in re.finditer(r'^const\s+(\w+)\s*=\s*(?=`|[A-Za-z_])', text, re.M):
            tokens, offset, expect_value = [], match.end(), True
            while token := TOKEN.match(text, offset):
                literal, identifier, plus = token.groups()
                if bool(plus) == expect_value:
                    break
                tokens.append(('literal', literal[1:-1].replace('\r', '')) if literal else
                              ('reference', identifier) if identifier else ('plus', '+'))
                offset = token.end()
                expect_value = bool(plus)
            if tokens and not expect_value:
                values[match[1]] = tokens
    return values


def render_constant(values, name, active=()):
    if name in active:
        raise ValueError('循环 UI 常量: ' + name)
    if name not in values:
        raise ValueError('未找到 UI 字符串常量: ' + name)
    return ''.join(value if kind == 'literal' else
                   render_constant(values, value, (*active, name))
                   for kind, value in values[name] if kind != 'plus')


def refresh_manifest(root):
    public = root / 'frontend/public'
    files = {p.relative_to(public).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
             for p in sorted(public.rglob('*')) if p.is_file()}
    if not {'index.html', 'login.html'} <= files.keys():
        raise ValueError('缺少前端页面')
    (root / 'frontend/ASSETS.json').write_text(json.dumps(files, indent=2) + '\n')
    return len(files)


def export(root=ROOT):
    public = root / 'frontend/public'
    public.mkdir(parents=True, exist_ok=True)
    static = public / 'static'
    static.mkdir(exist_ok=True)
    values = constants(root / 'src')
    for page, constant in (('index', 'indexHTML'), ('login', 'loginHTML')):
        html = render_constant(values, constant)
        sequence = {'script': 0, 'style': 0}

        def externalize(match):
            kind, contents = match[1], match[2]
            sequence[kind] += 1
            suffix = 'js' if kind == 'script' else 'css'
            filename = f'{page}-{sequence[kind]}.{suffix}'
            (static / filename).write_text(contents.strip() + '\n')
            return (f'<script src="static/{filename}"></script>' if kind == 'script' else
                    f'<link rel="stylesheet" href="static/{filename}">')

        html = re.sub(r'<(script|style)>(.*?)</\1>', externalize, html, flags=re.S)
        (public / (page + '.html')).write_text(html)
    shutil.copytree(root / 'src/assets', public / 'assets', dirs_exist_ok=True)
    count = refresh_manifest(root)
    print(f'已导出独立前端：{public}（{count} 个文件）')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--refresh-manifest', action='store_true', help='刷新资源校验清单，保留独立前端修改')
    mode.add_argument('--from-backend', action='store_true', help='用 Go 内嵌兼容页面覆盖独立前端；仅用于主动重新导入')
    args = parser.parse_args()
    if args.refresh_manifest:
        print('已更新前端资源校验清单：' + str(refresh_manifest(args.root.resolve())) + ' 个文件')
    elif args.from_backend:
        export(args.root.resolve())
    else:
        from frontend import verify_assets
        verify_assets(args.root.resolve())
        print('独立前端资源校验通过（未修改文件）')
