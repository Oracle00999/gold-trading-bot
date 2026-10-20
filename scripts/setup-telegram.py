from pathlib import Path


def required(label: str) -> str:
    value = input(label).strip()
    if not value or any(c.isspace() for c in value):
        raise SystemExit("A non-empty value without spaces is required.")
    return value


root = Path(__file__).resolve().parent.parent
local = root / ".local"
local.mkdir(mode=0o700, exist_ok=True)
token = required("Telegram bot token: ")
chat_id = required("Telegram chat ID: ")
path = local / "telegram.env"
path.write_text(f"TELEGRAM_BOT_TOKEN={token}\nTELEGRAM_CHAT_ID={chat_id}\n")
path.chmod(0o600)
print("Telegram configuration saved in .local/telegram.env (gitignored). Restart gold-bot to enable it.")
