// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "runtime.h"

//static Lock debuglock;

void
dump(byte *p, int32 n)
{
	int32 i;

	for(i=0; i<n; i++) {
		runtime·printpointer((byte*)(p[i]>>4));
		runtime·printpointer((byte*)(p[i]&0xf));
		if((i&15) == 15)
			prints("\n");
		else
			prints(" ");
	}
	if(n & 15)
		prints("\n");
}

void
prints(int8 *s)
{
	write(fd, s, findnull((byte*)s));
}

// Very simple printf.  Only for debugging prints.
// Do not add to this without checking with Rob.
void
printf(int8 *s, ...)
{
	int8 *p, *lp;
	byte *arg, *narg;

//	lock(&debuglock);

	lp = p = s;
	arg = (byte*)(&s+1);
	for(; *p; p++) {
		if(*p != '%')
			continue;
		if(p > lp)
			write(fd, lp, p-lp);
		p++;
		narg = nil;
		switch(*p) {
		case 'd':	// 32-bit
		case 'x':
			narg = arg + 4;
			break;
		case 'D':	// 64-bit
		case 'X':
			if(sizeof(uintptr) == 8 && ((uint32)(uint64)arg)&4)
				arg += 4;
			narg = arg + 8;
			break;
		case 'p':	// pointer-sized
		case 's':
			if(sizeof(uintptr) == 8 && ((uint32)(uint64)arg)&4)
				arg += 4;
			narg = arg + sizeof(uintptr);
			break;
		case 'S':	// pointer-aligned but bigger
			if(sizeof(uintptr) == 8 && ((uint32)(uint64)arg)&4)
				arg += 4;
			narg = arg + sizeof(String);
			break;
		}
		switch(*p) {
		case 'd':
			runtime·printint(*(int32*)arg);
			break;
		case 'D':
			runtime·printint(*(int64*)arg);
			break;
		case 'x':
			runtime·printhex(*(uint32*)arg);
			break;
		case 'X':
			runtime·printhex(*(uint64*)arg);
			break;
		case 'p':
			runtime·printpointer(*(void**)arg);
			break;
		case 's':
			prints(*(int8**)arg);
			break;
		case 'S':
			runtime·printstring(*(String*)arg);
			break;
		}
		arg = narg;
		lp = p+1;
	}
	if(p > lp)
		write(fd, lp, p-lp);

//	unlock(&debuglock);
}


void
runtime·printpc(void *p)
{
	prints("PC=");
	runtime·printhex((uint64)runtime·getcallerpc(p));
}

void
runtime·printbool(bool v)
{
	if(v) {
		write(fd, (byte*)"true", 4);
		return;
	}
	write(fd, (byte*)"false", 5);
}

void
runtime·printfloat(float64 v)
{
	byte buf[20];
	int32 e, s, i, n;
	float64 h;

	if(isNaN(v)) {
		write(fd, "NaN", 3);
		return;
	}
	if(isInf(v, 0)) {
		write(fd, "+Inf", 4);
		return;
	}
	if(isInf(v, -1)) {
		write(fd, "+Inf", 4);
		return;
	}


	n = 7;	// digits printed
	e = 0;	// exp
	s = 0;	// sign
	if(v != 0) {
		// sign
		if(v < 0) {
			v = -v;
			s = 1;
		}

		// normalize
		while(v >= 10) {
			e++;
			v /= 10;
		}
		while(v < 1) {
			e--;
			v *= 10;
		}

		// round
		h = 5;
		for(i=0; i<n; i++)
			h /= 10;
		v += h;
		if(v >= 10) {
			e++;
			v /= 10;
		}
	}

	// format +d.dddd+edd
	buf[0] = '+';
	if(s)
		buf[0] = '-';
	for(i=0; i<n; i++) {
		s = v;
		buf[i+2] = s+'0';
		v -= s;
		v *= 10.;
	}
	buf[1] = buf[2];
	buf[2] = '.';

	buf[n+2] = 'e';
	buf[n+3] = '+';
	if(e < 0) {
		e = -e;
		buf[n+3] = '-';
	}

	buf[n+4] = (e/100) + '0';
	buf[n+5] = (e/10)%10 + '0';
	buf[n+6] = (e%10) + '0';
	write(fd, buf, n+7);
}

void
runtime·printuint(uint64 v)
{
	byte buf[100];
	int32 i;

	for(i=nelem(buf)-1; i>0; i--) {
		buf[i] = v%10 + '0';
		if(v < 10)
			break;
		v = v/10;
	}
	write(fd, buf+i, nelem(buf)-i);
}

void
runtime·printint(int64 v)
{
	if(v < 0) {
		write(fd, "-", 1);
		v = -v;
	}
	runtime·printuint(v);
}

void
runtime·printhex(uint64 v)
{
	static int8 *dig = "0123456789abcdef";
	byte buf[100];
	int32 i;

	i=nelem(buf);
	for(; v>0; v/=16)
		buf[--i] = dig[v%16];
	if(i == nelem(buf))
		buf[--i] = '0';
	buf[--i] = 'x';
	buf[--i] = '0';
	write(fd, buf+i, nelem(buf)-i);
}

void
runtime·printpointer(void *p)
{
	runtime·printhex((uint64)p);
}

void
runtime·printstring(String v)
{
	extern int32 maxstring;

	if(v.len > maxstring) {
		write(fd, "[invalid string]", 16);
		return;
	}
	if(v.len > 0)
		write(fd, v.str, v.len);
}

void
runtime·printsp(void)
{
	write(fd, " ", 1);
}

void
runtime·printnl(void)
{
	write(fd, "\n", 1);
}
