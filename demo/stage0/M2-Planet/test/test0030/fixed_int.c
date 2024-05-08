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

#include <stdint.h>

struct s
{
	uint8_t a;
	uint8_t b;
	uint16_t c;
	uint32_t d;
};

int main() {
	uint8_t u8a = 200;
	uint8_t u8b = 100;
	uint8_t u8c = u8a + u8b;
	if(u8c - 44) return 1;

	uint16_t u16a = 65535;
	uint16_t u16b = 400;
	uint16_t u16c = u16a + u16b;
	if(u16c - 399) return 2;

	uint32_t u32a = 2147483647;
	uint32_t u32b = 2147483647;
	uint32_t u32c = u32a + u32b + 3;
	if(u32c - 1) return 3;

	struct s t;
	t.c = 1;
	t.d = 2147483647;
	t.b = 3;
	t.a = 4;
	if(t.a + t.b + t.c + t.d - 2147483647 - 1 - 3 - 4) return 4;

	u16c = u8c;
	if(u16c - 44) return 5;

	int16_t i16a = u8a;
	if(i16a - 200) return 6;

	return 0;
}
