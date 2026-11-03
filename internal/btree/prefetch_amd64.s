#include "textflag.h"

// func prefetch(addr unsafe.Pointer)
TEXT ·prefetch(SB), NOSPLIT|NOFRAME, $0-8
	MOVQ	addr+0(FP), AX
	PREFETCHT0	(AX)
	RET

// func prefetch2(addr unsafe.Pointer)
TEXT ·prefetch2(SB), NOSPLIT|NOFRAME, $0-8
	MOVQ	addr+0(FP), AX
	PREFETCHT0	(AX)
	PREFETCHT0	64(AX)
	RET

// func prefetch4(addr unsafe.Pointer)
TEXT ·prefetch4(SB), NOSPLIT|NOFRAME, $0-8
	MOVQ	addr+0(FP), AX
	PREFETCHT0	(AX)
	PREFETCHT0	64(AX)
	PREFETCHT0	128(AX)
	PREFETCHT0	192(AX)
	RET
