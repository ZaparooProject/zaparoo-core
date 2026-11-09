// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Builds the vendored libfaad (faad2 2.11.4, third_party/faad2) as one
// translation unit: cgo only compiles C files in the package directory.
// This is the floating-point build with HE-AAC v1/v2 (SBR and PS); DRM,
// SSR and the fixed-point build are off. The defines stand in for the
// configure results faad2's CMake build hardcodes for every platform.
//
// Code from FAAD2 is copyright (c) Nero AG, www.nero.com

#define HAVE_INTTYPES_H 1
#define HAVE_MEMCPY 1
#define HAVE_STRING_H 1
#define HAVE_STRINGS_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_LRINTF 1
#define PACKAGE_VERSION "2.11.4"

#include "faad2_prefix.h"

#include "third_party/faad2/libfaad/bits.c"
#include "third_party/faad2/libfaad/cfft.c"
#include "third_party/faad2/libfaad/common.c"
#include "third_party/faad2/libfaad/decoder.c"
#include "third_party/faad2/libfaad/drc.c"
#include "third_party/faad2/libfaad/error.c"
#include "third_party/faad2/libfaad/filtbank.c"
#include "third_party/faad2/libfaad/hcr.c"
#include "third_party/faad2/libfaad/huffman.c"
#include "third_party/faad2/libfaad/ic_predict.c"
#include "third_party/faad2/libfaad/is.c"
#include "third_party/faad2/libfaad/lt_predict.c"
#include "third_party/faad2/libfaad/mdct.c"
#include "third_party/faad2/libfaad/mp4.c"
#include "third_party/faad2/libfaad/ms.c"
#include "third_party/faad2/libfaad/output.c"
#include "third_party/faad2/libfaad/pns.c"
#include "third_party/faad2/libfaad/ps_dec.c"
#include "third_party/faad2/libfaad/ps_syntax.c"
#include "third_party/faad2/libfaad/pulse.c"
#include "third_party/faad2/libfaad/rvlc.c"
#include "third_party/faad2/libfaad/sbr_dct.c"
#include "third_party/faad2/libfaad/sbr_dec.c"
#include "third_party/faad2/libfaad/sbr_e_nf.c"
#include "third_party/faad2/libfaad/sbr_fbt.c"
#include "third_party/faad2/libfaad/sbr_hfadj.c"
#include "third_party/faad2/libfaad/sbr_hfgen.c"
#include "third_party/faad2/libfaad/sbr_huff.c"
#include "third_party/faad2/libfaad/sbr_qmf.c"
#include "third_party/faad2/libfaad/sbr_syntax.c"
#include "third_party/faad2/libfaad/sbr_tf_grid.c"
#include "third_party/faad2/libfaad/specrec.c"
#include "third_party/faad2/libfaad/syntax.c"
#include "third_party/faad2/libfaad/tns.c"
