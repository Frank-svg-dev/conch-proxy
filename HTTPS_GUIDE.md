# HTTPS/TLS Configuration Guide

## Overview

This project supports both HTTP and HTTPS connections. HTTPS is optional and can be enabled via configuration.

## Configuration

### Environment Variables

Add the following variables to your `.env` file:

```bash
# Enable HTTPS/TLS
ENABLE_TLS=true

# Path to TLS certificate file (default: server.crt)
TLS_CERT_FILE=server.crt

# Path to TLS private key file (default: server.key)
TLS_KEY_FILE=server.key
```

## Setting Up HTTPS

### Option 1: Self-Signed Certificate (Development)

For local development, you can generate a self-signed certificate:

```bash
# Make the script executable
chmod +x scripts/generate-cert.sh

# Run the script
./scripts/generate-cert.sh
```

This will create:
- `server.crt` - SSL certificate
- `server.key` - Private key

**Note**: Self-signed certificates will cause browser warnings. This is normal for development.

### Option 2: Let's Encrypt (Production)

For production, use Let's Encrypt for free SSL certificates:

```bash
# Install certbot
sudo apt-get install certbot

# Generate certificate
sudo certbot certonly --standalone -d yourdomain.com

# Copy certificates
sudo cp /etc/letsencrypt/live/yourdomain.com/fullchain.pem server.crt
sudo cp /etc/letsencrypt/live/yourdomain.com/privkey.pem server.key
```

### Option 3: Commercial Certificate

If you have a commercial SSL certificate:

1. Copy your certificate files to the project directory:
   - Certificate file → `server.crt`
   - Private key file → `server.key`

2. Update `.env` if using different filenames:
   ```bash
   TLS_CERT_FILE=/path/to/your/cert.crt
   TLS_KEY_FILE=/path/to/your/key.key
   ```

## Running the Server

### HTTP Mode (Default)

```bash
# Set ENABLE_TLS=false or omit the variable
go run cmd/server/main.go
```

Server will start on: `http://localhost:8080`

### HTTPS Mode

```bash
# Set ENABLE_TLS=true in .env
ENABLE_TLS=true go run cmd/server/main.go
```

Server will start on: `https://localhost:8080`

## Testing HTTPS

### Using curl

```bash
# Test HTTPS endpoint
curl -k https://localhost:8080/health

# Test API with HTTPS
curl -k -X POST https://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

Note: The `-k` flag allows insecure connections (useful for self-signed certificates).

### Using Browser

When accessing `https://localhost:8080` with a self-signed certificate:
1. Your browser will show a security warning
2. Click "Advanced" → "Proceed to localhost (unsafe)"
3. This is safe for local development

## Production Deployment

For production deployment:

1. **Use valid SSL certificates** (Let's Encrypt or commercial)
2. **Update domain names** in certificate
3. **Configure firewall** to allow HTTPS traffic (port 443)
4. **Set up reverse proxy** (nginx, Apache) if needed
5. **Enable HSTS** for additional security

### Example nginx Configuration

```nginx
server {
    listen 443 ssl;
    server_name yourdomain.com;

    ssl_certificate /path/to/server.crt;
    ssl_certificate_key /path/to/server.key;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## Troubleshooting

### Certificate Not Found

**Error**: `open server.crt: no such file or directory`

**Solution**: Generate certificates or update `TLS_CERT_FILE` path in `.env`

### Permission Denied

**Error**: `permission denied` when accessing certificate files

**Solution**: 
```bash
chmod 644 server.crt
chmod 600 server.key
```

### Browser Security Warning

**Issue**: "Your connection is not private" warning

**Solution**: This is normal for self-signed certificates. For production, use a valid SSL certificate from Let's Encrypt or a commercial CA.

## Security Best Practices

1. **Never commit** private keys to version control
2. **Use strong certificates** in production (2048-bit or higher)
3. **Keep certificates updated** (renew before expiration)
4. **Use HTTPS only** in production environments
5. **Implement proper CORS** policies for web applications
6. **Monitor certificate expiration** and set up renewal reminders

## Port Configuration

By default, the server uses port 8080. You can change this:

```bash
# For HTTP
PORT=8080

# For HTTPS (standard HTTPS port)
PORT=443
```

When using port 443, you may need to run with sudo:

```bash
sudo PORT=443 go run cmd/server/main.go
```
