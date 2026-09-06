CLANG ?= clang
BPFTOOL ?= bpftool
UNAME_M := $(shell uname -m)
ARCH := $(shell echo $(UNAME_M) | sed 's/x86_64/x86/;s/aarch64/arm64/')

# vmlinux.h is generated from *this build machine's* kernel BTF, not
# committed (see .gitignore) -- CO-RE's whole model is that the resulting
# object's BTF relocation records get resolved against the actual *target*
# kernel's BTF at load time, so the vmlinux.h used here at compile time
# doesn't need to match where the .o eventually runs. It's self-contained
# (already declares struct ethhdr/iphdr/udphdr etc.), which is what lets
# packet_filter.c drop the old <linux/...> UAPI includes and, with them,
# the asm/types.h include-path workaround below used to need.
CFLAGS := -O2 -g -Wall -target bpf -D__TARGET_ARCH_$(ARCH) -I.

.PHONY: all clean

all: packet_filter.o

headers/vmlinux.h:
	$(BPFTOOL) btf dump file /sys/kernel/btf/vmlinux format c > $@

packet_filter.o: packet_filter.c headers/common.h headers/vmlinux.h
	$(CLANG) $(CFLAGS) -c packet_filter.c -o packet_filter.o

clean:
	rm -f packet_filter.o headers/vmlinux.h
