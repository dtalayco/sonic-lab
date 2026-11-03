#!/usr/bin/env python3
"""Turn a collect.py samples file (and optional events file) into a report.

  report.py RUN_DIR                 writes RUN_DIR/report.html
  report.py RUN_DIR --openmetrics   writes RUN_DIR/samples.om for
                                    `promtool tsdb create-blocks-from openmetrics`

RUN_DIR holds samples.jsonl and, if the tests recorded phases, events.jsonl.
"""

import argparse
import html
import json
import os
import sys


def load(run_dir):
    header, scrapes, events = {}, [], []
    with open(os.path.join(run_dir, 'samples.jsonl')) as f:
        for line in f:
            rec = json.loads(line)
            if 'samples' in rec:
                scrapes.append(rec)
            else:
                header = rec
    ev = os.path.join(run_dir, 'events.jsonl')
    if os.path.exists(ev):
        with open(ev) as f:
            events = [json.loads(line) for line in f if line.strip()]
    return header, scrapes, events


def series(scrapes, name, key):
    """Map key(labels) -> [(t_ms, value)] for samples named name."""
    out = {}
    for s in scrapes:
        for x in s['samples']:
            if x['name'] == name:
                out.setdefault(key(x['labels']), []).append((s['t'], x['value']))
    return out


def rate(points, scale=1.0):
    """Per-second rate of a counter; resets count as zero."""
    out = []
    for (t0, v0), (t1, v1) in zip(points, points[1:]):
        dt = (t1 - t0) / 1000
        if dt > 0:
            out.append((t1, max(0.0, (v1 - v0) / dt * scale)))
    return out


def phases(events):
    starts, out = {}, []
    for e in events:
        k = (e['label'], e['proto'])
        if e['event'] == 'start':
            starts[k] = e['t']
        elif e['event'] == 'end' and k in starts:
            out.append(dict(e, start=starts.pop(k)))
    return out


def openmetrics(scrapes, path):
    def labels(d):
        if not d:
            return ''
        return '{' + ','.join('%s="%s"' % (k, v.replace('\\', '\\\\').replace('"', '\\"'))
                              for k, v in sorted(d.items())) + '}'
    # Each metric family's samples must be together, after its TYPE line.
    # OpenMetrics names a counter family without the _total suffix.
    families = {}
    for s in scrapes:
        for x in s['samples']:
            families.setdefault(x['name'], []).append(
                '%s%s %r %.3f' % (x['name'], labels(x['labels']), x['value'], s['t'] / 1000))
    with open(path, 'w') as f:
        for n in sorted(families):
            if n.endswith('_total'):
                f.write('# TYPE %s counter\n' % n[:-6])
            else:
                f.write('# TYPE %s gauge\n' % n)
            f.write('\n'.join(families[n]) + '\n')
        f.write('# EOF\n')


def build(header, scrapes, events):
    t0 = scrapes[0]['t'] if scrapes else 0

    def rel(points):
        return [[round((t - t0) / 1000, 2), round(v, 3)] for t, v in points]

    charts = {'total': None, 'links': [], 'queues': [], 'homa': []}

    # Link throughput, bits/s per direction
    by_dir = series(scrapes, 'webmesh_link_bytes_total',
                    lambda l: (l.get('link', ''), l.get('from', ''), l.get('to', '')))
    links = {}
    for (link, frm, to), pts in by_dir.items():
        links.setdefault(link, []).append({'name': '%s → %s' % (frm, to), 'points': rate(pts, 8)})
    total = {}
    for link in sorted(links):
        dirs = sorted(links[link], key=lambda d: d['name'])
        charts['links'].append({'title': link, 'unit': 'bps',
                                'series': [{'name': d['name'], 'points': rel(d['points'])} for d in dirs]})
        for d in dirs:
            for t, v in d['points']:
                total[t] = total.get(t, 0) + v
    charts['total'] = {'title': 'Forwarded, all links and directions', 'unit': 'bps',
                       'series': [{'name': 'total', 'points': rel(sorted(total.items()))}]}

    # Queues: root qdisc backlog and drop rate per node:dev
    backlog = series(scrapes, 'tc_qdisc_backlog_bytes',
                     lambda l: (l['node'] + ':' + l['dev']) if l.get('parent') == 'root' else None)
    drops = series(scrapes, 'tc_qdisc_drops_total',
                   lambda l: (l['node'] + ':' + l['dev']) if l.get('parent') == 'root' else None)
    for port in sorted(k for k in backlog if k):
        charts['queues'].append({'title': port + ' backlog', 'unit': 'bytes',
                                 'series': [{'name': 'backlog', 'points': rel(backlog[port])}]})
        if port in drops:
            charts['queues'].append({'title': port + ' drops', 'unit': 'per_s',
                                     'series': [{'name': 'drops/s', 'points': rel(rate(drops[port]))}]})

    for name, title in (('homa_resent_packets_total', 'Homa packets resent'),
                        ('homa_rpc_timeouts_total', 'Homa RPC timeouts')):
        s = series(scrapes, name, lambda l: '')
        if s.get(''):
            charts['homa'].append({'title': title + ' per second', 'unit': 'per_s',
                                   'series': [{'name': title, 'points': rel(rate(s['']))}]})

    ph = [{'label': p['label'], 'proto': p['proto'], 'gbps': p.get('gbps'), 'rtt': p.get('rtt', {}),
           'start': round((p['start'] - t0) / 1000, 2), 'end': round((p['t'] - t0) / 1000, 2)}
          for p in phases(events)]
    span = round((scrapes[-1]['t'] - t0) / 1000, 2) if scrapes else 0
    return {'run': header.get('run', {}), 'span': span, 'charts': charts, 'phases': ph}


TEMPLATE = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'report.html.in')


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('run_dir')
    p.add_argument('--openmetrics', action='store_true', help='write samples.om instead of the report')
    a = p.parse_args()
    header, scrapes, events = load(a.run_dir)
    if not scrapes:
        sys.exit('no samples in %s' % a.run_dir)
    if a.openmetrics:
        out = os.path.join(a.run_dir, 'samples.om')
        openmetrics(scrapes, out)
    else:
        data = build(header, scrapes, events)
        title = data['run'].get('name') or os.path.basename(os.path.abspath(a.run_dir))
        with open(TEMPLATE) as f:
            page = f.read()
        page = page.replace('{{TITLE}}', html.escape(title)) \
                   .replace('{{DATA}}', json.dumps(data).replace('</', '<\\/'))
        out = os.path.join(a.run_dir, 'report.html')
        with open(out, 'w') as f:
            f.write(page)
    print(out)


if __name__ == '__main__':
    main()
