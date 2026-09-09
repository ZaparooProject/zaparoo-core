// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

#include <stdio.h>
#include <string.h>
#include <libusb.h>
#include <usb.h>
#include <nfc/nfc.h>

// Exercise the linked native archives without opening a reader or submitting USB
// transfers. Explicit checks remain active when release flags define NDEBUG.
int main(void) {
    const struct libusb_version *version = libusb_get_version();
    if (!version) return 1;
    printf("libusb %u.%u.%u; libnfc %s\n", version->major, version->minor,
           version->micro, nfc_version());
    struct libusb_init_option option = { .option = LIBUSB_OPTION_NO_DEVICE_DISCOVERY };
    libusb_context *usb = NULL;
    if (libusb_init_context(&usb, &option, 1) != 0 || !usb) return 2;
    for (int i = 0; i < 1000; i++) {
        struct libusb_transfer *transfer = libusb_alloc_transfer(i % 8);
        if (!transfer || transfer->num_iso_packets != 0) return 3;
        transfer->num_iso_packets = i % 8;
        libusb_set_iso_packet_lengths(transfer, 64);
        for (int packet = 0; packet < transfer->num_iso_packets; packet++) {
            if (transfer->iso_packet_desc[packet].length != 64) return 6;
        }
        libusb_free_transfer(transfer);
    }
    libusb_exit(usb);
    usb_init();
    nfc_context *nfc = NULL;
    nfc_init(&nfc);
    if (!nfc) return 4;
    nfc_target target = {0};
    target.nm.nmt = NMT_ISO14443A;
    target.nm.nbr = NBR_106;
    target.nti.nai.szUidLen = 4;
    target.nti.nai.abtAtqa[0] = 0x04;
    target.nti.nai.btSak = 0x08;
    for (int i = 0; i < 1000; i++) {
        target.nti.nai.abtUid[0] = (unsigned char)i;
        char *text = NULL;
        int length = str_nfc_target(&text, &target, true);
        if (!text || length <= 0 || (size_t)length != strlen(text)) return 5;
        nfc_free(text);
    }
    nfc_exit(nfc);
    puts("Native library smoke checks passed");
    return 0;
}
