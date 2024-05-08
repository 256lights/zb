/* Copyright (C) 2022 Andrius Štikonas
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

#include <stdlib.h>

struct s
{
	char x;
	int y;
	int z[3];
};

struct s a;

int main() {
	a.x = 3;
	a.y = 5;
	if(a.x * a.y != 15) return 1;
	if((&a)->y != 5) return 2;
	a.z[0] = 1;
	a.z[1] = 2;
	a.z[2] = 3;
	if (a.z[0] + a.z[1] + a.z[2] != 6) return 3;

	struct s b;
	b.x = 3;
	b.y = 5;
	if(b.x * b.y != 15) return 4;
	if((&b)->y != 5) return 5;
	b.z[0] = 1;
	b.z[1] = 2;
	b.z[2] = 3;
	if(b.z[0] + b.z[1] + b.z[2] != 6) return 6;

	struct s* p = calloc(2, sizeof(struct s));
	p->x = 3;
	p[1].y = 4;
	if(p[0].x != 3) return 7;
	if(p[1].y != 4) return 8;

	return 0;
}
