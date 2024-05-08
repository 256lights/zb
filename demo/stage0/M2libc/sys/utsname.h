/* Copyright (C) 2024 Jeremiah Orians
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

#ifndef _UTSNAME_H
#define _UTSNAME_H
struct utsname
{
	char sysname[65];    /* Operating system name (e.g., "Linux") */
	char nodename[65];   /* Name within "some implementation-defined network" */
	char release[65];    /* Operating system release (e.g., "2.6.28") */
	char version[65];    /* Operating system version */
	char machine[65];    /* Hardware identifier */
};

#ifdef __M2__
/* already exists in $ARCH/linux/unistd.c */
#else
int uname(struct utsname*);
#endif
#endif
