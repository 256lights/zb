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

#include <stddef.h>

char* strcpy(char* dest, char const* src)
{
	int i = 0;

	while (0 != src[i])
	{
		dest[i] = src[i];
		i = i + 1;
	}
	dest[i] = 0;

	return dest;
}


char* strncpy(char* dest, char const* src, size_t count)
{
	if(0 == count) return dest;
	size_t i = 0;
	while(0 != src[i])
	{
		dest[i] = src[i];
		i = i + 1;
		if(count == i) return dest;
	}

	while(i <= count)
	{
		dest[i] = 0;
		i = i + 1;
	}

	return dest;
}


char* strcat(char* dest, char const* src)
{
	int i = 0;
	int j = 0;
	while(0 != dest[i]) i = i + 1;
	while(0 != src[j])
	{
		dest[i] = src[j];
		i = i + 1;
		j = j + 1;
	}
	dest[i] = 0;
	return dest;
}


char* strncat(char* dest, char const* src, size_t count)
{
	size_t i = 0;
	size_t j = 0;
	while(0 != dest[i]) i = i + 1;
	while(0 != src[j])
	{
		if(count == j)
		{
			dest[i] = 0;
			return dest;
		}
		dest[i] = src[j];
		i = i + 1;
		j = j + 1;
	}
	dest[i] = 0;
	return dest;
}


size_t strlen(char const* str )
{
	size_t i = 0;
	while(0 != str[i]) i = i + 1;
	return i;
}


size_t strnlen_s(char const* str, size_t strsz )
{
	size_t i = 0;
	while(0 != str[i])
	{
		if(strsz == i) return i;
		i = i + 1;
	}
	return i;
}


int strcmp(char const* lhs, char const* rhs )
{
	int i = 0;
	while(0 != lhs[i])
	{
		if(lhs[i] != rhs[i]) return lhs[i] - rhs[i];
		i = i + 1;
	}

	return lhs[i] - rhs[i];
}


int strncmp(char const* lhs, char const* rhs, size_t count)
{
	size_t i = 0;
	while(count > i)
	{
		if(0 == lhs[i]) break;
		if(lhs[i] != rhs[i]) return lhs[i] - rhs[i];
		i = i + 1;
	}

	return 0;
}


char* strchr(char const* str, int ch)
{
	char* p = str;
	while(ch != p[0])
	{
		if(0 == p[0]) return NULL;
		p = p + 1;
	}
	if(0 == p[0]) return NULL;
	return p;
}


char* strrchr(char const* str, int ch)
{
	char* p = str;
	int i = 0;
	while(0 != p[i]) i = i + 1;
	while(ch != p[i])
	{
		if(0 == i) return NULL;
		i = i - 1;
	}
	return (p + i);
}


size_t strspn(char const* dest, char const* src)
{
	if(0 == dest[0]) return 0;
	int i = 0;
	while(NULL != strchr(src, dest[i])) i = i + 1;
	return i;
}


size_t strcspn(char const* dest, char const* src)
{
	int i = 0;
	while(NULL == strchr(src, dest[i])) i = i + 1;
	return i;
}


char* strpbrk(char const* dest, char const* breakset)
{
	char* p = dest;
	char* s;
	while(0 != p[0])
	{
		s = strchr(breakset, p[0]);
		if(NULL != s) return strchr(p,  s[0]);
		p = p + 1;
	}
	return p;
}


void* memset(void* dest, int ch, size_t count)
{
	if(NULL == dest) return dest;
	size_t i = 0;
	char* s = dest;
	while(i < count)
	{
		s[i] = ch;
		i = i + 1;
	}
	return dest;
}


void* memcpy(void* dest, void const* src, size_t count)
{
	if(NULL == dest) return dest;
	if(NULL == src) return NULL;

	char* s1 = dest;
	char const* s2 = src;
	size_t i = 0;
	while(i < count)
	{
		s1[i] = s2[i];
		i = i + 1;
	}
	return dest;
}

void* memmove(void* dest, void const* src, size_t count)
{
	if (dest < src) return memcpy (dest, src, count);
	char *p = dest;
	char const *q = src;
	count = count - 1;
	while (count >= 0)
	{
		p[count] = q[count];
		count = count - 1;
	}
	return dest;
}


int memcmp(void const* lhs, void const* rhs, size_t count)
{
	if(0 == count) return 0;
	size_t i = 0;
	count = count - 1;
	char const* s1 = lhs;
	char const* s2 = rhs;
	while(i < count)
	{
		if(s1[i] != s2[i]) break;
		i = i + 1;
	}
	return (s1[i] - s2[i]);
}
