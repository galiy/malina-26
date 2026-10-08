echo 'root:malina' | chpasswd
passwd -u root >/dev/null 2>&1 || true

install -d -m 0755 /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/10-malina.conf <<'EOF'
PasswordAuthentication yes
PermitRootLogin yes
EOF
