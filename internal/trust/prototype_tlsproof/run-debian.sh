set -e
apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq ca-certificates >/dev/null 2>&1
/p/tlsproof-linux gen /work >/dev/null
cp /work/os-ca.pem /usr/local/share/ca-certificates/prototype-os-ca.crt && update-ca-certificates >/dev/null 2>&1
/p/tlsproof-linux run /work 2>/dev/null
