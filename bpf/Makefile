CLANG ?= clang
UNAME_M := $(shell uname -m)
ARCH := $(shell echo $(UNAME_M) | sed 's/x86_64/x86/;s/aarch64/arm64/')

# packet_filter.c builds against headers/vmlinux_min.h, a small hand-
# maintained header (not host-BTF-generated -- see that file's own top
# comment for why), so this build has no dependency on bpftool or a real
# Linux kernel's /sys/kernel/btf/vmlinux -- it works the same inside a
# `docker build` as it does on a dev machine or in CI.
CFLAGS := -O2 -g -Wall -target bpf -D__TARGET_ARCH_$(ARCH) -I.

.PHONY: all clean

all: packet_filter.o

packet_filter.o: packet_filter.c headers/common.h headers/vmlinux_min.h
	$(CLANG) $(CFLAGS) -c packet_filter.c -o packet_filter.o

clean:
	rm -f packet_filter.o
