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
	"exutil"
	u "ubftab"

	atmi "github.com/endurox-dev/endurox-go"
)

//Generate error that connection is not found
//@param buf	UBF buffer
//@param id_comp	Compiled/composite connection id (can be simple too)
//@param code		Error code
//@param messages	Customer error message
func GenError(ac *atmi.ATMICtx, buf *atmi.TypedUBF, id_comp int64, code int, message string) {

	sz, _ := buf.BSizeof()
	ac.TpLogDebug("Allocating: %d", sz)
	ac.BInit(buf, sz)

	if id_comp > 0 {
		buf.BChg(u.EX_NETCONNID, 0, id_comp)
	}

	buf.BChg(u.EX_NERROR_CODE, 0, code)
	buf.BChg(u.EX_NERROR_MSG, 0, message)
}

//Generate error that connection is not found
//@param buf	UBF buffer
//@param id_comp	Compiled/composite connection id (can be simple too)
//@param code		Error code
//@param messages	Customer error message
//@return <UBF buffer if allocated>,  ATMI Error code ir failure
func GenErrorUBF(ac *atmi.ATMICtx, id_comp int64, code int, message string) (*atmi.TypedUBF, atmi.ATMIError) {

	buf, errA := ac.NewUBF(1024)

	if nil != errA {
		ac.TpLogError("Failed to allocate UBF buffer: %s", errA.Message())
		return nil, errA
	}

	if id_comp > 0 {
		buf.BChg(u.EX_NETCONNID, 0, id_comp)
	}

	buf.BChg(u.EX_NERROR_CODE, 0, code)
	buf.BChg(u.EX_NERROR_MSG, 0, message)

	return buf, nil

}

//Dispatcht the XATMI call (in own go routine)
//@param pool XATMI Pool
//@param nr	XATMI client number
//@param ctxData	Context data for request
//@param buf	ATMI buffer with request data
func XATMIDispatchCall(pool *XATMIPool, nr int, ctxData *atmi.TPSRVCTXDATA, buf *atmi.TypedUBF) {

	ret := SUCCEED
	ac := pool.ctxs[nr]
	var connid int64 = 0
	var corr string = ""

	defer func() {

		//Put back the channel
		pool.freechan <- nr

		if SUCCEED == ret {
			buf.TpLogPrintUBF(atmi.LOG_DEBUG, "Reply with SUCCEED")
			ac.TpReturn(atmi.SUCCEED, 0, buf, 0)
		} else {
			buf.TpLogPrintUBF(atmi.LOG_DEBUG, "Reply with FAIL")
			ac.TpReturn(atmi.TPFAIL, 0, buf, 0)
		}
	}()

	ac.TpLogInfo("About to restore context data in goroutine...")
	ac.TpSrvSetCtxData(ctxData, 0)

	//OK so our context have a call, now do something with it

	connid, _ = buf.BGetInt64(u.EX_NETCONNID, 0)
	corr, _ = buf.BGetString(u.EX_NETCORR, 0)

	if RR_PERS_ASYNC_INCL_CORR == MReqReply || RR_PERS_CONN_EX2NET == MReqReply {
		if GetOpenConnectionCount() > 0 {
			//Get the connection to send message to
			/* If connection id specified, then get that one.. */
			var con *ExCon
			var block DataBlock
			var errA atmi.ATMIError

			block.data, errA = buf.BGetByteArr(u.EX_NETDATA, 0)

			if nil != errA {
				ac.TpLogError("Missing EX_NETDATA: %s!", errA.Message())
				//Reply with failure

				GenError(ac, buf, atmi.NEMANDATORY, 0,
					"Mandatory field EX_NETDATA missing!")
				ret = FAIL
				return

			}

			if connid == 0 {
				con = GetOpenConnection(ac)
			} else {
				con = GetConnectionByID(ac, connid)
			}

			if nil == con {
				GenError(ac, buf, 0, atmi.NENOCONN,
					"No open connections available")
				ret = FAIL
				return
			}

			block.corr = corr
			block.atmi_out_conn_id = connid
			block.tstamp_sent = exutil.GetEpochMillis()

			//Register in tables (if needed by config)
			haveMCorrWaiter := false
			if MReqReply == RR_PERS_ASYNC_INCL_CORR {
				//Only in asyn mode
				//In process can be only in one waiting list
				if corr != "" {
					ac.TpLogInfo("Adding request to corr table, by "+
						"correlator: [%s]", corr)
					MCorrWaiterMutex.Lock()
					MCorrWaiter[corr] = &block
					MCorrWaiterMutex.Unlock()
					haveMCorrWaiter = true
				}
			}

			//If we work on sync way, only one data exchange over
			//The single channel, then lets add to id waiter list
			haveMConWaiter := false
			if MReqReply == RR_PERS_CONN_EX2NET {
				ac.TpLogInfo("Adding request to conn table, by "+
					"comp_id: [%d]", con.id_comp)
				MConWaiterMutex.Lock()
				MConWaiter[con.id_comp] = &block
				MConWaiterMutex.Unlock()
				haveMConWaiter = true
			}

			ac.TpLogWarn("About to send data...")
			con.outgoing <- &block

			//If we are in correl or sync mode we need to wait data
			//block back...

			if corr != "" || MReqReply == RR_PERS_CONN_EX2NET {
				ac.TpLogWarn("Waiting for reply: correl [%s] "+
					"req_reply %d", corr, MReqReply)
				//Override the reply buffer
				//No more checks... as tout should be already generated.
				buf = <-block.atmi_chan

				//Remove waiter from lists...
				ac.TpLogInfo("Got reply back")

				if haveMCorrWaiter {
					ac.TpLogInfo("Removing request from corr table, by "+
						"correlator: [%s]", corr)
					MCorrWaiterMutex.Lock()
					delete(MCorrWaiter, corr)
					MCorrWaiterMutex.Unlock()
				}

				if haveMConWaiter {
					ac.TpLogInfo("Request from conn table, by "+
						"comp_id: [%d]", con.id_comp)
					MConWaiterMutex.Lock()
					delete(MConWaiter, con.id_comp)
					MConWaiterMutex.Unlock()
				}
			}
		} else {
			//Reply - no connection
			GenError(ac, buf, 0, atmi.NENOCONN,
				"No open connections available")
			ret = FAIL
			return
		}
	} else if RR_NONPERS_EX2NET == MReqReply {
		ac.TpLogInfo("Non persistent mode, one connection per message. " +
			"Try to connect")

		//So we are about to open channel, get the connection id
		//Add connection to compiled connection list as normal
		//get the connection and send stuff away. The connection Handler
		//should know already that conn must be closed by req_reply

		var con ExCon
		var block DataBlock
		var errA atmi.ATMIError

		block.data, errA = buf.BGetByteArr(u.EX_NETDATA, 0)

		if nil != errA {
			ac.TpLogError("Missing EX_NETDATA: %s!", errA.Message())
			//Reply with failure

			GenError(ac, buf, 0, atmi.NEMANDATORY,
				"Mandatory field EX_NETDATA missing!")
			ret = FAIL
			return

		}

		block.corr = corr
		block.atmi_out_conn_id = connid
		block.tstamp_sent = exutil.GetEpochMillis()

		//1. Prepare connection block
		MConnMutex.Lock()
		con.id, con.id_stamp, con.id_comp = GetNewConnectionId()

		if con.id == FAIL {
			ac.TpLogError("Failed to get connection id - max reached?")
			ret = FAIL
			MConnMutex.Unlock()
		}

		//2. Add to hash

		MConnectionsSimple[con.id] = &con
		MConnectionsComp[con.id_comp] = &con
		MConnMutex.Unlock()

		//3. and spawn the routine...
		//TODO: We need to pass in here channel to which reply if
		//Connection did not succeed.
		go GoDial(&con, &block)

		//4. Now try to send stuff out?
		buf = <-block.atmi_chan

		ac.TpLogInfo("Got reply back")
	}
}

//TODO: Allow to broadcast message over all open connections