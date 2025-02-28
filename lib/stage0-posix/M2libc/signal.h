/* Copyright (C) 2021 Andrius Štikonas
 * This file is part of M2-Planet.
 *
 * M2-Planet is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * M2-Planet is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with M2-Planet.  If not, see <http://www.gnu.org/licenses/>.
 */

#ifndef _SIGNAL_H
#define _SIGNAL_H

#define CLD_EXITED      1       /* child has exited */
#define CLD_KILLED      2       /* child was killed */
#define CLD_DUMPED      3       /* child terminated abnormally */
#define CLD_TRAPPED     4       /* traced child has trapped */
#define CLD_STOPPED     5       /* child has stopped */
#define CLD_CONTINUED   6       /* stopped child has continued */

struct siginfo_t {
	int si_signo;
#ifdef __SI_SWAP_ERRNO_CODE
	int si_code;
	int si_errno;
#else
	int si_errno;
	int si_code;
#endif
	int si_pid;
	int si_uid;
	int si_status;
	int si_utime;
	int si_stime;
};

#endif
