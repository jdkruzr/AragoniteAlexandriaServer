import os
import importlib.util
import hashlib
import io
import tarfile
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("restore_cold", SOURCE / "restore-cold.py")
restore = importlib.util.module_from_spec(spec)
spec.loader.exec_module(restore)

class ColdBackupTest(unittest.TestCase):
    def test_manifest_archive_allowlist_and_quoted_boox(self):
        with tempfile.TemporaryDirectory() as tmp:
            bundle = Path(tmp)
            for name in restore.REQUIRED:
                (bundle / name).write_text('1' if name == 'backup-format' else '')
            for name in ('postgres.tar', 'objects.tar'):
                with tarfile.open(bundle / name, 'w'):
                    pass
            def manifest():
                (bundle / 'SHA256SUMS').write_text(''.join(
                    hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n'
                    for p in sorted(bundle.iterdir()) if p.name != 'SHA256SUMS'))
            manifest()
            restore.validate(bundle)
            with tarfile.open(bundle / 'surprise.tar', 'w') as archive:
                info = tarfile.TarInfo('../escape')
                info.size = 1
                archive.addfile(info, io.BytesIO(b'x'))
            manifest()
            with self.assertRaisesRegex(ValueError, 'Unexpected backup archive'):
                restore.validate(bundle)
            (bundle / 'surprise.tar').unlink()
            for setting in ('export ALEXANDRIA_BOOX_ENABLED="true" # enabled',
                            "ALEXANDRIA_BOOX_ENABLED='true'", 'ALEXANDRIA_BOOX_ENABLED=${ENABLED}'):
                (bundle / 'deployment.env').write_text(setting)
                manifest()
                with self.assertRaises(ValueError):
                    restore.validate(bundle)

    def run_backup(self, fail=False, forced_stop=False):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in ('backup-cold.sh', 'common.sh', 'compose.yml', 'data-dir.yml', 'boox.yml', 'boox-data-dir.yml'):
                shutil.copyfile(SOURCE / name, root / name)
            for name in ('postgres', 'objects', 'couchbase', 'backups', 'bin', 'private'):
                (root / name).mkdir()
            for name in ('postgres', 'objects', 'couchbase'):
                (root / name / 'sample').write_text(name)
            (root / 'private/owner-password').write_text('test-only')
            (root / 'private/gateway.json').write_text('{}')
            (root / '.env').write_text(f'''ALEXANDRIA_DATA_DIR={root}
ALEXANDRIA_POSTGRES_DIR={root}/postgres
ALEXANDRIA_OBJECT_DIR={root}/objects
ALEXANDRIA_COUCHBASE_DIR={root}/couchbase
ALEXANDRIA_BACKUP_DIR={root}/backups
ALEXANDRIA_BOOX_ENABLED=true
ALEXANDRIA_BOOX_GATEWAY_CONFIG={root}/private/gateway.json
ALEXANDRIA_SETTINGS_KEY=test-only
''')
            commands = {
                'id': '#!/bin/sh\necho 0\n',
                'docker': '''#!/bin/sh
printf '%s\n' "$*" >> "$TEST_ROOT/calls"
case "$1" in
compose)
 case " $* " in
 *" version "*) exit 0;;
 *" stop "*) touch "$TEST_ROOT/stopped";;
 *" ps --status running "*) if [ ! -f "$TEST_ROOT/stopped" ]; then echo application; fi; exit 0;;
 *" ps "*) echo application;;
 esac;;
inspect)
 case "$*" in
 *State.ExitCode*) echo "/application ${TEST_EXIT_CODE:-0} false";;
 *) echo sha256:test;;
 esac;;
image) if [ "$2" = save ]; then printf image > "$5"; fi;;
esac
''',
                'tar': '#!/bin/sh\n' + ('exit 1\n' if fail else 'exec /usr/bin/tar "$@"\n'),
            }
            for name, body in commands.items():
                (root / 'bin' / name).write_text(body)
                (root / 'bin' / name).chmod(0o755)
            env = dict(os.environ, PATH=str(root / 'bin') + ':' + os.environ['PATH'], TEST_ROOT=str(root), TEST_EXIT_CODE='137' if forced_stop else '0')
            result = subprocess.run(['sh', str(root / 'backup-cold.sh')], env=env, capture_output=True, text=True)
            calls = (root / 'calls').read_text()
            self.assertIn(' up -d', calls)
            self.assertIn(' stop --timeout 60', calls)
            if fail or forced_stop:
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(list((root / 'backups').glob('*.partial'))), 1)
                if forced_stop:
                    self.assertIn('Forced/OOM container stop', result.stderr)
                    partial = next((root / 'backups').iterdir())
                    self.assertFalse((partial / 'postgres.tar').exists())
                    self.assertIn('137', (partial / 'stop-status.txt').read_text())
                return
            self.assertEqual(result.returncode, 0, result.stderr)
            bundle = next((root / 'backups').iterdir())
            self.assertFalse(bundle.name.endswith('.partial'))
            for name in ('application-image.tar', 'application-image-id', 'owner-password', 'deployment.env', 'postgres.tar', 'objects.tar', 'couchbase.tar', 'boox-gateway.json'):
                self.assertTrue((bundle / name).is_file(), name)
            subprocess.run(['sha256sum', '-c', 'SHA256SUMS'], cwd=bundle, check=True, capture_output=True)
            manifest = restore.validate(bundle)
            target = root / 'restored'
            restore.extract(bundle, target, manifest)
            self.assertEqual((target / 'postgres/sample').read_text(), 'postgres')
            with self.assertRaises(FileExistsError): restore.extract(bundle, target, manifest)
            (bundle / 'owner-password').write_text('tampered')
            with self.assertRaises(ValueError): restore.validate(bundle)
            (bundle / 'owner-password').write_text('test-only')
            with tarfile.open(bundle / 'postgres.tar', 'w') as archive:
                info=tarfile.TarInfo('../escape'); info.size=1
                archive.addfile(info, io.BytesIO(b'x'))
            checksum=hashlib.sha256((bundle / 'postgres.tar').read_bytes()).hexdigest()
            lines=(bundle / 'SHA256SUMS').read_text().splitlines()
            (bundle / 'SHA256SUMS').write_text('\n'.join(checksum+'  ./postgres.tar' if line.endswith('./postgres.tar') else line for line in lines)+'\n')
            with self.assertRaises(ValueError): restore.validate(bundle)

    def test_complete_backup(self): self.run_backup()
    def test_copy_failure_restarts_without_publishing(self): self.run_backup(True)
    def test_forced_stop_restarts_without_publishing(self): self.run_backup(forced_stop=True)

if __name__ == '__main__': unittest.main()
