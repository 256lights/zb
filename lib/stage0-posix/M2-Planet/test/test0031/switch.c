/* Copyright (C) 2023 Jeremiah Orians
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

int match(char* a, char* b);

int main(int argc, char** argv)
{
	char* a = argv[1];
	int b = 0;

	if(match("test1", a))
	{
		b = 1;
	}
	else if(match("test2", a))
	{
		b = 3;
	}
	else if(match("test3", a))
	{
		b = 4;
	}
	else if(match("test4", a))
	{
		b = 6;
	}
	else if(match("test5", a))
	{
		b = 9;
	}
	else if(match("test6", a))
	{
		b = 8;
	}
	else if(match("test7", a))
	{
		b = 7;
	}

	int i = 31;
	switch(b)
	{
		case 0: return 111;
		case 1:
		case 2: return 122;
		case 3: break;
		case 4: i = 42;
		case 5: i = i + 1;
		        break;
		case 7: i = i + 2;
		case 8: i = i + 1;
		case 9:
			switch(i)
			{
				case 31: return 133;
				case 32: i = 77;
				case 33: break;
				default: i = 144;
			}
			i = i + 7;
			break;
		default: return 155;
	}

	return i;
}
