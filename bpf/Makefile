CLANG ?= clang
UNAME_M := $(shell uname -m)
ARCH := $(shell echo $(UNAME_M) | sed 's/x86_64/x86/;s/aarch64/arm64/')

# clang -target bpf doesn't match the host triple, so it skips the host's
# default multiarch include search path (unlike a native x86_64-linux-gnu
# compile) — without this, <linux/types.h>'s `#include <asm/types.h>` fails
# even though libc6-dev/linux-libc-dev installed it, just under
# /usr/include/<triplet>/asm rather than /usr/include/asm.
CFLAGS := -O2 -g -Wall -target bpf -D__TARGET_ARCH_$(ARCH) -I. -I/usr/include/$(UNAME_M)-linux-gnu

.PHONY: all clean

all: packet_filter.o

packet_filter.o: packet_filter.c headers/common.h
	$(CLANG) $(CFLAGS) -c packet_filter.c -o packet_filter.o

clean:
	rm -f packet_filter.o
