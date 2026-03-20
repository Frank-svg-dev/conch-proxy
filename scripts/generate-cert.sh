#!/bin/bash

# Generate self-signed SSL certificate for development

echo "Generating self-signed SSL certificate..."

# Generate private key
openssl genrsa -out server.key 2048

# Generate certificate signing request
openssl req -new -key server.key -out server.csr -subj "/C=US/ST=State/L=City/O=Organization/CN=localhost"

# Generate self-signed certificate
openssl x509 -req -days 365 -in server.csr -signkey server.key -out server.crt

# Clean up CSR file
rm server.csr

echo "SSL certificate generated successfully!"
echo "Files created:"
echo "  - server.crt (certificate)"
echo "  - server.key (private key)"
echo ""
echo "To use HTTPS, set ENABLE_TLS=true in your .env file"
echo "Note: This is a self-signed certificate for development only."
echo "Your browser will show a security warning which you can safely ignore for local development."
