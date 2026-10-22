/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.

patch_panel.c — TC ingress eBPF program for the webmesh patch panel.

Build:
    clang -O2 -target bpf -c patch_panel.c -o patch_panel.o

For each link A:EthernetX <-> B:EthernetY, the webmesh namespace holds
two veth endpoints, wm-ethA and wm-ethB, where the reconciler assigns
A and B per endpoint. This program is attached to the ingress of both.
On ingress, it looks up the incoming ifindex in link_map to get the
peer ifindex, then calls bpf_redirect_peer() which
delivers the packet to the veth-peer of that ifindex (i.e., the switch
interface in the remote switch's namespace).
*/

#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

/*
 * Old-style (non-BTF) map definitions using the bpf_map_def struct
 * already provided by bpf_helpers.h. The section name "maps" (without
 * a leading dot) causes the cilium/ebpf library to parse the binary
 * bpf_map_def entries directly, without requiring BTF. Using ".maps"
 * (with a dot) would trigger BTF-based parsing which requires
 * CONFIG_DEBUG_INFO_BTF in the kernel.
 */

/*
 * link_map: ingress ifindex (webmesh ns) → peer ifindex (webmesh ns)
 *
 * bpf_redirect_peer(peer_ifidx) delivers the skb to the veth-peer of
 * the interface with that ifindex. Since both webmesh-side endpoints
 * have their peers in switch namespaces, redirecting to the remote
 * webmesh endpoint's ifindex causes delivery to the remote switch.
 */
struct bpf_map_def SEC("maps") link_map = {
    .type        = BPF_MAP_TYPE_HASH,
    .key_size    = sizeof(__u32),  /* ingress ifindex */
    .value_size  = sizeof(__u32),  /* peer ifindex    */
    .max_entries = 1024,
};

/* Per-link statistics for observability */
struct link_stats {
    __u64 packets;
    __u64 bytes;
};

struct bpf_map_def SEC("maps") stats_map = {
    .type        = BPF_MAP_TYPE_HASH,
    .key_size    = sizeof(__u32),
    .value_size  = sizeof(struct link_stats),
    .max_entries = 1024,
};

SEC("tc/ingress")
int patch_panel(struct __sk_buff *skb)
{
    __u32 in_if = skb->ingress_ifindex;
    __u32 *peer = bpf_map_lookup_elem(&link_map, &in_if);

    if (peer == NULL)
        return TC_ACT_SHOT; /* unmapped — drop */

    /* Update per-link stats */
    struct link_stats *s = bpf_map_lookup_elem(&stats_map, &in_if);
    if (s != NULL) {
        s->packets++;
        s->bytes += skb->len;
    }

    /* Redirect to the peer of the remote webmesh endpoint.
     * bpf_redirect_peer(X) sends the skb to the veth-peer of ifindex X. */
    return bpf_redirect_peer(*peer, 0);
}

char _license[] SEC("license") = "GPL";
