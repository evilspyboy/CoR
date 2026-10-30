import os
import urllib.request
import json

ls_addr = os.environ.get("ANTIGRAVITY_LS_ADDRESS", "localhost:28990")
csrf = os.environ.get("ANTIGRAVITY_CSRF_TOKEN", "")

url = f"http://{ls_addr}/exa.language_server_pb.LanguageServerService/JetboxWriteState"
data = json.dumps({"userConfig": {"plugins": {"council-of-reeds": {"enabled": True}}}}).encode("utf-8")

req = urllib.request.Request(
    url,
    data=data,
    headers={
        "Content-Type": "application/json",
        "x-codeium-csrf-token": csrf
    }
)

try:
    with urllib.request.urlopen(req) as resp:
        print("JetboxWriteState status:", resp.status)
        print("Response:", resp.read().decode())
except Exception as e:
    print("Error enabling plugin:", e)

