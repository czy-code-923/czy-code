#!/usr/bin/env python3
"""Redirect one domain's ACME HTTP-01 challenges to the Starhub webroot."""
import argparse
import ipaddress
import re
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlsplit

DOMAIN = re.compile(r'(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$')
CHALLENGE = re.compile(r'/\.well-known/acme-challenge/[A-Za-z0-9_-]{1,256}$')

def domain(value):
    value = value.lower().rstrip('.')
    if not DOMAIN.fullmatch(value):
        raise ValueError('invalid DNS name')
    return value

class Server(HTTPServer):
    def get_request(self):
        conn, addr = super().get_request()
        conn.settimeout(5)
        return conn, addr

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass  # Do not log challenge tokens.
    def do_GET(self):
        host = self.headers.get('Host', '').lower().removesuffix(':80')
        if host != self.server.domain or not CHALLENGE.fullmatch(self.path):
            self.send_error(404)
            return
        self.send_response(302)
        self.send_header('Location', self.server.target + self.path)
        self.send_header('Content-Length', '0')
        self.end_headers()

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--domain', required=True)
    p.add_argument('--target', required=True, help='HTTPS origin of the Starhub management gateway')
    p.add_argument('--listen', default='0.0.0.0')
    p.add_argument('--port', type=int, default=80)
    args = p.parse_args()
    origin = urlsplit(args.target)
    if origin.scheme != 'https' or not origin.hostname or origin.username or origin.password or origin.path or origin.query or origin.fragment or origin.port not in (None, 443):
        p.error('target must be an HTTPS DNS origin without a path or custom port')
    host = domain(origin.hostname)
    ipaddress.IPv4Address(args.listen)
    if not 1 <= args.port <= 65535:
        p.error('invalid port')
    server = Server((args.listen, args.port), Handler)
    server.domain = domain(args.domain)
    server.target = 'https://' + host
    server.serve_forever()

if __name__ == '__main__':
    main()

