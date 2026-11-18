/*
 * BW_Midi_Sequencer - MIDI Sequencer for C++
 *
 * Copyright (c) 2015-2026 Vitaly Novichkov <admin@wohlnet.ru>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the "Software"),
 * to deal in the Software without restriction, including without limitation the
 * rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
 * sell copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
 * OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM,
 * DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
 * ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
 * DEALINGS IN THE SOFTWARE.
 */

#pragma once
#ifndef BW_MIDISEQ_ERRORSTR_IMPL_HPP
#define BW_MIDISEQ_ERRORSTR_IMPL_HPP

#include <cstdarg>
#include <cstdio>
#include <cstring>

#include "../midi_sequencer.hpp"


/**********************************************************************************
 *                             ErrString                                          *
 **********************************************************************************/


BW_MidiSequencer::ErrString::ErrString() :
    size(0)
{
    err[0] = 0;
}

void BW_MidiSequencer::ErrString::clear()
{
    err[0] = 0;
    size = 0;
}

void BW_MidiSequencer::ErrString::set(const char *str)
{
    size = 0;
    append(str);
}

void BW_MidiSequencer::ErrString::append(const char *str)
{
    while(size < 1000 && *str)
        err[size++] = *(str++);

    err[size] = 0;
}

void BW_MidiSequencer::ErrString::append(const char *str, size_t len)
{
    while(size < 1000 && len-- > 0)
        err[size++] = *(str++);

    err[size] = 0;
}

/* Zaparoo: the upstream code assigned vsnprintf's result to size, so appendFmt
 * lost the earlier text's length, and a truncated result (the untruncated
 * length, or -1 from some Windows C runtimes) pushed size past the buffer.
 * Advance size by what was actually written and keep it within the buffer. */
static void bwErrStringAppendV(char *err, size_t &size, const char *fmt, va_list args)
{
    if(size >= 1000)
        return;

    int written = vsnprintf(err + size, 1001 - size, fmt, args);
    err[1000] = 0;

    if(written < 0)
        size += std::strlen(err + size);
    else
        size += static_cast<size_t>(written);

    if(size > 1000)
        size = 1000;
    err[size] = 0;
}

void BW_MidiSequencer::ErrString::setFmt(const char *fmt, ...)
{
    va_list args;
    size = 0;
    err[0] = 0;
    va_start(args, fmt);
    bwErrStringAppendV(err, size, fmt, args);
    va_end(args);
}

void BW_MidiSequencer::ErrString::appendFmt(const char *fmt, ...)
{
    va_list args;
    va_start(args, fmt);
    bwErrStringAppendV(err, size, fmt, args);
    va_end(args);
}

#endif /* BW_MIDISEQ_ERRORSTR_IMPL_HPP */
