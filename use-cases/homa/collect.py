#!/usr/bin/env python3
"""Sample lab counters once a second into a JSON-lines file.

Run as root inside the Docker VM. Each line after the header is one scrape:

  {"t": <ms>, "samples": [{"name": ..., "labels": {...}, "value": ...}, ...]}

Samples are raw counters and gauges in Prometheus naming, so a file can be
converted with `report.py --openmetrics` and loaded into Prometheus later.

Sources:
  webmesh   its /metrics endpoint (per-link bytes and packets)
  --tc      qdisc stats for NODE:DEV, where NODE is a container, netns:NAME,
            or root
  --homa    /proc/net/homa_metrics, summed over cores
"""

import argparse
import json
import re
import signal
import subprocess
import sys
import time
import urllib.request

HOMA_DEFAULT = re.compile(r'^(packets_(sent|rcvd)_\w+|resent_packets|.*timeouts?|.*_bytes)$')


def parse_prometheus(text):
    out = []
    for line in text.splitlines():
        if not line or line.startswith('#'):
            continue
        m = re.match(r'^(\w+)(?:\{(.*)\})?\s+(\S+)$', line)
        if not m:
            continue
        labels = {}
        if m.group(2):
            for k, v in re.findall(r'(\w+)="((?:[^"\\]|\\.)*)"', m.group(2)):
                labels[k] = v.replace('\\"', '"').replace('\\n', '\n').replace('\\\\', '\\')
        out.append({'name': m.group(1), 'labels': labels, 'value': float(m.group(3))})
    return out


def ns_prefix(node, pids):
    if node == 'root':
        return []
    if node.startswith('netns:'):
        return ['ip', 'netns', 'exec', node[6:]]
    if node not in pids:
        pids[node] = subprocess.check_output(
            ['docker', 'inspect', '-f', '{{.State.Pid}}', node], text=True).strip()
    return ['nsenter', '-t', pids[node], '-n']


def tc_samples(target, pids):
    node, dev = target.rsplit(':', 1)
    try:
        raw = subprocess.check_output(ns_prefix(node, pids) + ['tc', '-s', '-j', 'qdisc', 'show', 'dev', dev],
                                      text=True, stderr=subprocess.DEVNULL)
    except (subprocess.CalledProcessError, FileNotFoundError):
        pids.pop(node, None)
        return []
    out = []
    for q in json.loads(raw or '[]'):
        labels = {'node': node, 'dev': dev, 'kind': q.get('kind', ''),
                  'handle': q.get('handle', ''), 'parent': q.get('parent', 'root')}
        for name, key in (('tc_qdisc_bytes_total', 'bytes'), ('tc_qdisc_packets_total', 'packets'),
                          ('tc_qdisc_drops_total', 'drops'), ('tc_qdisc_backlog_bytes', 'backlog'),
                          ('tc_qdisc_backlog_packets', 'qlen')):
            if key in q:
                out.append({'name': name, 'labels': labels, 'value': float(q[key])})
    return out


def homa_samples(want):
    sums = {}
    try:
        with open('/proc/net/homa_metrics') as f:
            for line in f:
                parts = line.split()
                if len(parts) < 2 or parts[0] in ('core', 'time_cycles', 'cpu_khz'):
                    continue
                if want.match(parts[0]):
                    try:
                        sums[parts[0]] = sums.get(parts[0], 0) + int(parts[1])
                    except ValueError:
                        pass
    except FileNotFoundError:
        return []
    # Homa's metrics are cumulative counts
    return [{'name': 'homa_' + k + '_total', 'labels': {}, 'value': float(v)} for k, v in sorted(sums.items())]


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('-o', '--output', required=True, help='JSON-lines file to write')
    p.add_argument('-i', '--interval', type=float, default=1.0, help='seconds between scrapes')
    p.add_argument('--webmesh', default='http://localhost:8092', help='webmesh base URL ("" to skip)')
    p.add_argument('--tc', action='append', default=[], metavar='NODE:DEV', help='qdisc stats to collect')
    p.add_argument('--homa', action='store_true', help='collect Homa metrics')
    p.add_argument('--homa-match', default=HOMA_DEFAULT.pattern, help='regex of Homa metric names')
    p.add_argument('--label', action='append', default=[], metavar='KEY=VALUE', help='run metadata')
    a = p.parse_args()

    stop = []
    signal.signal(signal.SIGTERM, lambda *_: stop.append(1))
    signal.signal(signal.SIGINT, lambda *_: stop.append(1))
    homa_re = re.compile(a.homa_match)
    pids = {}

    with open(a.output, 'a', buffering=1) as out:
        out.write(json.dumps({'run': dict(kv.split('=', 1) for kv in a.label),
                              'start_ms': int(time.time() * 1000), 'interval': a.interval,
                              'sources': {'webmesh': a.webmesh, 'tc': a.tc, 'homa': a.homa}}) + '\n')
        next_t = time.monotonic()
        while not stop:
            t = int(time.time() * 1000)
            samples = []
            if a.webmesh:
                try:
                    with urllib.request.urlopen(a.webmesh + '/metrics', timeout=2) as r:
                        samples += parse_prometheus(r.read().decode())
                except OSError as e:
                    print('webmesh: %s' % e, file=sys.stderr)
            for target in a.tc:
                samples += tc_samples(target, pids)
            if a.homa:
                samples += homa_samples(homa_re)
            out.write(json.dumps({'t': t, 'samples': samples}, separators=(',', ':')) + '\n')
            next_t += a.interval
            time.sleep(max(0, next_t - time.monotonic()))


if __name__ == '__main__':
    main()
