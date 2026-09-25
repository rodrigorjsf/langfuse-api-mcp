set -e
/p/tlsproof-linux gen /work >/dev/null
cp /work/os-ca.pem /etc/pki/ca-trust/source/anchors/prototype-os-ca.pem && update-ca-trust
/p/tlsproof-linux run /work 2>/dev/null
