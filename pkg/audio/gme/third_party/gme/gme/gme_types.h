#ifndef GME_TYPES_H
#define GME_TYPES_H

/*
 * Zaparoo Core build configuration, replacing the upstream default: only
 * the emulators pkg/audio/gme compiles are registered.
 */
#define USE_GME_GBS
#define USE_GME_HES
#define USE_GME_KSS
#define USE_GME_NSF
#define USE_GME_NSFE
#define USE_GME_SPC

#endif /* GME_TYPES_H */
