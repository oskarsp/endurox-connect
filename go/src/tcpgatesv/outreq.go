/*
** Enduro/X -> World (OUT) Request handling...
**
** @file outreq.go
** -----------------------------------------------------------------------------
** Enduro/X Middleware Platform for Distributed Transaction Processing
** Copyright (C) 2015, ATR Baltic, SIA. All Rights Reserved.
** This software is released under one of the following licenses:
** GPL or ATR Baltic's license for commercial use.
** -----------------------------------------------------------------------------
** GPL license:
**
** This program is free software; you can redistribute it and/or modify it under
** the terms of the GNU General Public License as published by the Free Software
** Foundation; either version 2 of the License, or (at your option) any later
** version.
**
** This program is distributed in the hope that it will be useful, but WITHOUT ANY
** WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
** PARTICULAR PURPOSE. See the GNU General Public License for more details.
**
** You should have received a copy of the GNU General Public License along with
** this program; if not, write to the Free Software Foundation, Inc., 59 Temple
** Place, Suite 330, Boston, MA 02111-1307 USA
**
** -----------------------------------------------------------------------------
** A commercial use license is available from ATR Baltic, SIA
** contact@atrbaltic.com
** -----------------------------------------------------------------------------
 */
package main

import (
	atmi "github.com/endurox-dev/endurox-go"
)

//Dispatcht the XATMI call (in own go routine)
func XATMIDispatchCall(pool *XATMIPool, nr int, ctxData *atmi.TPSRVCTXDATA, buf *atmi.TypedUBF) {

	ret := SUCCEED
	ac := pool.ctxs[nr]

	defer func() {
		if SUCCEED == ret {
			ac.TpReturn(atmi.SUCCEED, 0, buf, 0)
		} else {
			ac.TpReturn(atmi.TPFAIL, 0, buf, 0)
		}
	}()

	ac.TpSrvSetCtxData(ctxData, 0)

	//OK so our context have a call, now do something with it

	//Put back the channel
	pool.freechan <- nr
}
