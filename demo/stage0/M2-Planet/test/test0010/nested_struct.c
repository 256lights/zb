/* Copyright (C) 2016 Jeremiah Orians
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
#include<stdlib.h>
#include<stdio.h>

struct bar
{
	char z;
	int x;
	int y;
};

struct foo
{
	struct foo* next;
	struct foo* prev;
	struct bar* a;
};

void print_hex(int a, int count)
{
	if(count <= 0) return;
	print_hex(a >> 4, count - 1);
	putchar((a & 15) + 48);
}

int main()
{
	struct foo* a = malloc(sizeof(struct foo));
	struct foo* b = malloc(sizeof(struct foo));
	struct bar* c = malloc(sizeof(struct bar));
	struct bar* d = malloc(sizeof(struct bar));

	c->x = 0x35419896;
	c->y = 0x57891634;
	d->x = 0x13579246;
	d->y = 0x64297531;
	a->a = c;
	b->a = d;
	a->next = b;
	a->prev = b;
	b->next = a;
	b->prev = a;

	print_hex(a->next->next->a->x, 8);
	print_hex(b->prev->prev->a->y, 8);
	print_hex(b->next->a->x, 8);
	print_hex(b->prev->a->y, 8);
	putchar(10);
	return sizeof(struct foo);
}
