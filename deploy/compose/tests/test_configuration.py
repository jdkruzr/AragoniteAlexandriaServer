"""Resolve backup paths and engines without accessing a real container engine."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1]


def clean_env():
    return {k: v for k, v in os.environ.items()
            if not k.startswith('ALEXANDRIA_') and k not in ('COMPOSE', 'CONTAINER_ENGINE')}


class ConfigurationTest(unittest.TestCase):
    def test_backup_restore_paths(self):
        for kind in ('derived', 'quoted', 'explicit', 'environment', 'default'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                for name in ('backup.sh', 'restore.sh', 'common.sh'):
                    shutil.copyfile(SOURCE / name, root / name)
                fake = root / 'fake-compose'
                fake.write_text('#!/bin/sh\nprintf "%s|%s\\n" "$ALEXANDRIA_BACKUP_DIR" "$*" >> "$CALLS"\n')
                fake.chmod(0o755)
                expected = root / 'backups'
                config = ''
                if kind == 'derived':
                    config = f'ALEXANDRIA_DATA_DIR={root}\n'
                elif kind == 'quoted':
                    expected = root / 'with spaces'
                    config = f"ALEXANDRIA_BACKUP_DIR='{expected}'\n"
                elif kind == 'explicit':
                    expected = root / 'explicit'
                    config = f'ALEXANDRIA_BACKUP_DIR={expected}\n'
                env = dict(clean_env(), COMPOSE=str(fake), CALLS=str(root / 'calls'))
                if kind == 'environment':
                    expected = root / 'environment'
                    env['ALEXANDRIA_BACKUP_DIR'] = str(expected)
                else:
                    (root / '.env').write_text(config)
                for script, data in (('backup.sh', ''), ('restore.sh', 'restore\n')):
                    result = subprocess.run(['sh', str(root / script)], input=data,
                                            env=env, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue((expected / 'alexandria.dump').exists())
                expected_env = './backups' if kind == 'default' else str(expected)
                calls = (root / 'calls').read_text().splitlines()
                self.assertTrue(all(c.startswith(expected_env + '|') for c in calls), calls)
                self.assertTrue(any('pg_restore' in c for c in calls))

    def test_override_engine_selection(self):
        for command, override, expected in (
            ('docker compose', '', 'docker'),
            ('podman compose', '', 'podman'),
            ('docker-compose', '', 'docker'),
            ('podman-compose', '', 'podman'),
            ('custom-wrapper', 'podman', 'podman'),
            ('custom-wrapper', '', None),
            ('docker compose', 'invalid', None),
        ):
            with self.subTest(command=command, override=override), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                shutil.copyfile(SOURCE / 'common.sh', root / 'common.sh')
                for engine in ('docker', 'podman'):
                    path = root / engine
                    path.write_text(f'#!/bin/sh\nprintf "{engine} %s\\n" "$*"\n')
                    path.chmod(0o755)
                env = dict(clean_env(), PATH=str(root) + ':' + os.environ['PATH'],
                           COMPOSE=command, CONTAINER_ENGINE=override)
                result = subprocess.run(['sh', '-ec', '. ./common.sh; engine inspect synthetic'],
                                        cwd=root, env=env, capture_output=True, text=True)
                if expected:
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stdout.strip(), expected + ' inspect synthetic')
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(result.stdout, '')
                    self.assertIn('CONTAINER_ENGINE', result.stderr)


if __name__ == '__main__':
    unittest.main()
