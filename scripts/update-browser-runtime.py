#!/usr/bin/env python3
"""Maintainer-only: pin official Chrome for Testing archives and executables.

No runtime dependency on Python/Node/Playwright is introduced. Review and test
all updated pins before release. Existing --archive-dir ZIPs allow offline
regeneration with official metadata verification; missing ZIPs are downloaded
only from Google's published URLs.
"""
import argparse
import base64
import tempfile
import hashlib
import json
import pathlib
import re
import urllib.request
import zipfile


def digest_file(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--archive-dir', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path,
                        default=pathlib.Path(__file__).resolve().parents[1] / 'internal/browser/runtime-lock.json')
    args = parser.parse_args()
    if not re.fullmatch(r'\d+\.\d+\.\d+\.\d+', args.version):
        parser.error('version must have four numeric components')
    args.archive_dir.mkdir(parents=True, exist_ok=True)
    assets = []
    platforms = ['linux-arm64', 'linux64', 'mac-arm64', 'mac-x64', 'win32', 'win64']
    for kind in ['chrome-headless-shell', 'chrome']:
        for platform in platforms:
            name = f'{kind}-{platform}'
            url = f'https://storage.googleapis.com/chrome-for-testing-public/{args.version}/{platform}/{name}.zip'
            archive = args.archive_dir / (name + '.zip')
            # Official metadata also verifies that a cached ZIP belongs to
            # this requested version, rather than a previous update run.
            with urllib.request.urlopen(urllib.request.Request(url, method='HEAD'), timeout=30) as response:
                expected_size = int(response.headers['Content-Length'])
                hashes = ','.join(response.headers.get_all('x-goog-hash', []))
                expected_md5 = next(part.strip()[4:] for part in hashes.split(',') if part.strip().startswith('md5='))
            if not archive.exists():
                temporary = None
                try:
                    with tempfile.NamedTemporaryFile('wb', dir=args.archive_dir, prefix='.' + name, delete=False) as target:
                        temporary = pathlib.Path(target.name)
                        with urllib.request.urlopen(url, timeout=60) as source:
                            size = 0
                            while chunk := source.read(1024 * 1024):
                                size += len(chunk)
                                if size > 512 * 1024 * 1024:
                                    raise ValueError('runtime archive too large')
                                target.write(chunk)
                    temporary.replace(archive)
                finally:
                    if temporary is not None:
                        temporary.unlink(missing_ok=True)
            md5 = hashlib.md5()
            with archive.open('rb') as source:
                while chunk := source.read(1024 * 1024):
                    md5.update(chunk)
            if archive.stat().st_size != expected_size or base64.b64encode(md5.digest()).decode() != expected_md5:
                raise ValueError(f'archive does not match official metadata for {args.version}: {archive}')
            if kind == 'chrome-headless-shell':
                executable = f'{name}/chrome-headless-shell' + ('.exe' if platform.startswith('win') else '')
            elif platform.startswith('mac-'):
                executable = f'{name}/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing'
            else:
                executable = f'{name}/chrome' + ('.exe' if platform.startswith('win') else '')
            with zipfile.ZipFile(archive) as bundle:
                if bundle.testzip() is not None:
                    raise ValueError(f'corrupt archive: {archive}')
                executable_hash = hashlib.sha256(bundle.read(executable)).hexdigest()
            assets.append(dict(kind=kind, platform=platform, url=url, size=archive.stat().st_size,
                               sha256=digest_file(archive), executable_sha256=executable_hash))
    # Publish only after every platform is complete. A failed download cannot
    # leave a partial source manifest accepted by a subsequent build.
    temporary = None
    try:
        with tempfile.NamedTemporaryFile('w', dir=args.output.parent, prefix='.runtime-lock-', delete=False) as output:
            temporary = pathlib.Path(output.name)
            output.write(json.dumps(dict(version=args.version, assets=assets), indent=2) + '\n')
        temporary.replace(args.output)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


if __name__ == '__main__':
    main()
