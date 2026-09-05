#ifndef SENTINEL5G_BPF_COMMON_H
#define SENTINEL5G_BPF_COMMON_H

/* 3GPP GTP-U (user plane tunneling, e.g. N3/N9 interfaces). */
#define GTPU_PORT 2152

/* SIP signaling (VoIP/IMS control plane). */
#define SIP_PORT 5060

#define MAX_BLOCKLIST_ENTRIES 65536
#define MAX_RATE_ENTRIES 65536

/* Rolling window used by track_signal_rate() to bucket packet counts. */
#define SIGNALING_RATE_WINDOW_NS 1000000000ULL /* 1 second */

#endif /* SENTINEL5G_BPF_COMMON_H */
