#!/usr/bin/env python3
"""Verify a cold backup; optionally extract into a NEW offline staging directory.

Does not start containers, change endpoints, or overwrite a running deployment.
Run extraction as root or with `podman unshare` to preserve numeric ownership.
"""
import argparse
import hashlib
import os
from pathlib import Path, PurePosixPath
import shutil
import shlex
import tarfile

REQUIRED = {'backup-format', 'deployment.env', 'postgres.tar', 'objects.tar',
            'application-image.tar', 'application-image-id', 'images.txt', 'compose.yml', 'data-dir.yml'}
DATA_ARCHIVES = {'postgres.tar', 'objects.tar', 'couchbase.tar'}

def boox_enabled(config):
    # Read literal shell assignments without executing the backed-up environment.
    enabled = False
    for line in config.splitlines():
        tokens = shlex.split(line, comments=True)
        if tokens[:1] == ['export']:
            tokens = tokens[1:]
        for token in tokens:
            if token.startswith('ALEXANDRIA_BOOX_ENABLED='):
                value = token.partition('=')[2]
                if value not in ('true', 'false'):
                    raise ValueError('BOOX backup setting must be literal true or false')
                enabled = value == 'true'
    return enabled

def validate(bundle):
    manifest = {}
    for line in (bundle / 'SHA256SUMS').read_text().splitlines():
        digest, name = line.split('  ', 1)
        name = name.removeprefix('./')
        if len(digest) != 64 or any(c not in '0123456789abcdef' for c in digest):
            raise ValueError('Invalid checksum')
        if Path(name).name != name or name in manifest:
            raise ValueError('Unsafe or duplicate manifest name')
        path = bundle / name
        if path.is_symlink() or not path.is_file():
            raise ValueError('Missing or linked backup file')
        with path.open('rb') as stream:
            if hashlib.file_digest(stream, 'sha256').hexdigest() != digest:
                raise ValueError('Backup checksum mismatch: ' + name)
        manifest[name] = digest
    required = REQUIRED.copy()
    if not required <= manifest.keys():
        raise ValueError('Incomplete backup manifest: ' + ', '.join(sorted(required - manifest.keys())))
    unexpected = {name for name in manifest if name.endswith('.tar')} - DATA_ARCHIVES - {'application-image.tar'}
    if unexpected:
        raise ValueError('Unexpected backup archive: ' + ', '.join(sorted(unexpected)))
    if 'couchbase.tar' in manifest or boox_enabled((bundle / 'deployment.env').read_text()):
        required |= {'couchbase.tar', 'boox-gateway.json', 'boox.yml', 'boox-data-dir.yml'}
    if not required <= manifest.keys():
        raise ValueError('Incomplete backup manifest: ' + ', '.join(sorted(required - manifest.keys())))
    if (bundle / 'backup-format').read_text().strip() != '1':
        raise ValueError('Unsupported backup format')
    for name in DATA_ARCHIVES:
        if name not in manifest:
            continue
        with tarfile.open(bundle / name) as archive:
            for member in archive:
                path = PurePosixPath(member.name)
                if path.is_absolute() or '..' in path.parts or not (member.isfile() or member.isdir()):
                    raise ValueError('Unsafe archive member in ' + name)
    return manifest

def extract(bundle, destination, manifest):
    # mkdir without exist_ok is the overwrite guard, including an empty target.
    destination.mkdir(mode=0o700)
    for name in manifest:
        if name in DATA_ARCHIVES:
            target = destination / name.removesuffix('.tar')
            target.mkdir(mode=0o700)
            with tarfile.open(bundle / name) as archive:
                archive.extractall(target, numeric_owner=True, filter='fully_trusted')
        else:
            shutil.copyfile(bundle / name, destination / name)
            (destination / name).chmod(0o600)
    shutil.copyfile(bundle / 'SHA256SUMS', destination / 'source-SHA256SUMS')
    (destination / 'OFFLINE-RESTORE.txt').write_text(
        'Offline data only. Verify image identities, remap all paths/ports/volumes, '
        'restrict egress and configure the intended public endpoint before startup.\n')

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('bundle', type=Path)
    p.add_argument('--extract', type=Path, metavar='NEW_DIRECTORY')
    args = p.parse_args()
    os.umask(0o077)
    manifest = validate(args.bundle)
    print('Backup checksums and archive paths verified.')
    if args.extract:
        if os.geteuid() != 0:
            p.error('Extraction requires root or podman unshare for ownership preservation')
        extract(args.bundle, args.extract, manifest)
        print('Extracted offline; no services started.')

if __name__ == '__main__': main()
