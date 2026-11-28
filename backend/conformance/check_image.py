"""Start the packaged read binding for each profile and run the PostgreSQL vectors."""
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def docker(*args, **options):
    return subprocess.run(['docker', *args], text=True, **options)


def image_dsn(dsn):
    override = os.environ.get('AAVANAM_IMAGE_PG_DSN')
    if override:
        return override
    address = urllib.parse.urlsplit(dsn)
    if address.scheme in ('postgres', 'postgresql') and address.hostname in ('127.0.0.1', 'localhost', '::1'):
        credentials, separator, _ = address.netloc.rpartition('@')
        port = f':{address.port}' if address.port is not None else ''
        return address._replace(netloc=f'{credentials}{separator}host.docker.internal{port}').geturl()
    return dsn


def wait_for_read(url):
    end = time.monotonic() + 40
    while True:
        try:
            with urllib.request.urlopen(url + '/v1/about', timeout=1) as response:
                if response.status == 200:
                    return
        except (urllib.error.URLError, OSError):
            pass
        if time.monotonic() >= end:
            raise SystemExit(f'The image read listener at {url} did not start.')
        time.sleep(0.25)


def main(image, command):
    dsn = os.environ.get('AAVANAM_PG_DSN')
    if not dsn:
        raise SystemExit('Set AAVANAM_PG_DSN to a PostgreSQL 17 test database.')
    env = os.environ | {'AAVANAM_PG_DSN': image_dsn(dsn)}
    network = os.environ.get('AAVANAM_IMAGE_NETWORK')
    network_args = ['--network', network] if network else []
    containers = []
    urls = {}
    status = 1
    try:
        # The service declares its profile at start. A vector uses the base profile or one optional profile.
        for profile in ('', 'aavanam/layer/v1', 'aavanam/semantic-index/v1'):
            flags = ['--profile', profile] if profile else []
            container = docker('run', '-d', *network_args, '-p', '127.0.0.1::8437', '-e', 'AAVANAM_PG_DSN',
                               image, '--read-addr', '0.0.0.0:8437', '--context-life', '0', *flags,
                               env=env, stdout=subprocess.PIPE, check=True).stdout.strip()
            containers.append(container)
            address = docker('port', container, '8437/tcp', capture_output=True, check=True).stdout.strip()
            urls[profile] = 'http://' + address
            wait_for_read(urls[profile])
        status = subprocess.run(command, env=os.environ | {
            'AAVANAM_IMAGE_READ_URLS': json.dumps(urls), 'AAVANAM_READ_HTTP': '1',
        }).returncode
    finally:
        for container in containers:
            if status:
                docker('logs', container)
            docker('rm', '-f', container, stdout=subprocess.DEVNULL)
    return status


if __name__ == '__main__':
    raise SystemExit(main(sys.argv[1], sys.argv[2:]))
