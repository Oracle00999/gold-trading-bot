from pathlib import Path
import secrets
root = Path(__file__).resolve().parent.parent
local = root / '.local'
local.mkdir(mode=0o700, exist_ok=True)
env = local / 'postgres.env'
if not env.exists():
    password = secrets.token_hex(32)
    env.write_text('POSTGRES_PASSWORD=' + password + '\n')
    env.chmod(0o600)
else:
    password = env.read_text().strip().split('=', 1)[1]
url = local / 'database-url'
url.write_text('postgres://fx_trader:' + password + '@127.0.0.1:55432/fx_trader?sslmode=disable\n')
url.chmod(0o600)
print('Local database configuration ready; credentials are in .local (gitignored).')
