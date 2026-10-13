"""Optional provider lifecycle must not start indexing or unrelated services."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1]

class OllamaSetupTest(unittest.TestCase):
    def test_profile_selection(self):
        for enabled in (False, True):
            for boox in (False, True):
                with self.subTest(enabled=enabled, boox=boox), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    shutil.copyfile(SOURCE / 'common.sh', root / 'common.sh')
                    (root / '.env').write_text(
                        f'ALEXANDRIA_OLLAMA_ENABLED={str(enabled).lower()}\n'
                        f'ALEXANDRIA_BOOX_ENABLED={str(boox).lower()}\n'
                        'COMPOSE=echo\n')
                    result = subprocess.run(['sh', '-ec', '. ./common.sh; compose stop'], cwd=root,
                                            capture_output=True, text=True, check=True)
                    self.assertEqual('--profile embeddings' in result.stdout, enabled)
                    self.assertEqual('--profile boox' in result.stdout, boox)
                    self.assertTrue(result.stdout.rstrip().endswith('stop'))

    def setup_provider(self, pull_fails=False):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for f in ('common.sh', 'setup-ollama.sh'):
                shutil.copyfile(SOURCE / f, root / f)
            fake = root / 'compose'
            fake.write_text('''#!/bin/sh
printf '%s\\n' "$*" >> "$CALLS"
case " $* " in
 *" ollama pull "*) exit "${PULL_EXIT:-0}";;
esac
''')
            fake.chmod(0o755)
            config = f'COMPOSE={fake}\nALEXANDRIA_OLLAMA_ENABLED=false\n'
            (root / '.env').write_text(config)
            result = subprocess.run(['sh', str(root / 'setup-ollama.sh'), 'nomic-embed-text:v1.5'],
                                    env=dict(os.environ, CALLS=str(root / 'calls'), PULL_EXIT='1' if pull_fails else '0'),
                                    capture_output=True, text=True)
            calls = (root / 'calls').read_text()
            self.assertIn('up -d --no-deps ollama', calls)
            self.assertIn('exec -T ollama ollama list', calls)
            self.assertIn('exec -T ollama ollama pull nomic-embed-text:v1.5', calls)
            self.assertNotIn('alexandria', calls)
            self.assertEqual((root / '.env').read_text(), config)
            self.assertEqual(result.returncode != 0, pull_fails)
            self.assertEqual('Ollama is available' in result.stdout, not pull_fails)

    def test_setup_only_starts_provider(self): self.setup_provider()
    def test_download_failure_is_reported(self): self.setup_provider(True)

if __name__ == '__main__': unittest.main()
