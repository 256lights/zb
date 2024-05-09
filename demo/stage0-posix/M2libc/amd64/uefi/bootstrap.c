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

// CONSTANT stdin 0
// CONSTANT stdout 1
// CONSTANT stderr 2
// CONSTANT EOF 0xFFFFFFFF
// CONSTANT NULL 0
// CONSTANT EXIT_FAILURE 1
// CONSTANT EXIT_SUCCESS 0
// CONSTANT TRUE 1
// CONSTANT FALSE 0

/* UEFI */
// CONSTANT PAGE_SIZE 4096
// CONSTANT PAGE_NUM 16384
// CONSTANT USER_STACK_SIZE 8388608
// CONSTANT EFI_OPEN_PROTOCOL_BY_HANDLE_PROTOCOL 1
// CONSTANT EFI_FILE_MODE_READ 1
// CONSTANT EFI_FILE_MODE_WRITE 2
// CONSTANT EFI_FILE_READ_ONLY 1
// CONSTANT EFI_ALLOCATE_ANY_PAGES 0
// CONSTANT EFI_LOADER_DATA 2

void exit(unsigned value);

void* _image_handle;
void* _root_device;
void* __user_stack;
void* _malloc_start;
long _malloc_ptr;
long _brk_ptr;
int _argc;
char** _argv;

struct efi_simple_text_output_protocol
{
	void* reset;
	void* output_string;
	void* test_string;
	void* query_mode;
	void* set_mode;
	void* set_attribute;
	void* clear_screen;
	void* set_cursor;
	void* enable_cursor;
	void* mode;
};

struct efi_table_header
{
	unsigned signature;
	unsigned revision_and_header_size;
	unsigned crc32_and_reserved;
};

struct efi_boot_table
{
	struct efi_table_header header;

	/* Task Priority Services */
	void* raise_tpl;
	void* restore_tpl;

	/* Memory Services */
	void* allocate_pages;
	void* free_pages;
	void* get_memory_map;
	void* allocate_pool;
	void* free_pool;

	/* Event & Timer Services */
	void* create_event;
	void* set_timer;
	void* wait_for_event;
	void* signal_event;
	void* close_event;
	void* check_event;

	/* Protocol Handler Services */
	void* install_protocol_interface;
	void* reinstall_protocol_interface;
	void* uninstall_protocol_interface;
	void* handle_protocol;
	void* reserved;
	void* register_protocol_notify;
	void* locate_handle;
	void* locate_device_path;
	void* install_configuration_table;

	/* Image Services */
	void* load_image;
	void* start_image;
	void* exit;
	void* unload_image;
	void* exit_boot_services;

	/* Miscellaneous Services */
	void* get_next_monotonic_count;
	void* stall;
	void* set_watchdog_timer;

	/* DriverSupport Services */
	void* connect_controller;
	void* disconnect_controller;

	/* Open and Close Protocol Services */
	void* open_protocol;
	void* close_protocol;
	void* open_protocol_information;

	/* Library Services */
	void* protocols_per_handle;
	void* locate_handle_buffer;
	void* locate_protocol;
	void* install_multiple_protocol_interfaces;
	void* uninstall_multiple_protocol_interfaces;

	/* 32-bit CRC Services */
	void* copy_mem;
	void* set_mem;
	void* create_event_ex;
};

struct efi_system_table
{
	struct efi_table_header header;

	char* firmware_vendor;
	unsigned firmware_revision;
	void* console_in_handle;
	void* con_in;
	void* console_out_handle;
	struct efi_simple_text_output_protocol* con_out;
	void *standard_error_handle;
	struct efi_simple_text_output_protocol* std_err;
	void *runtime_services;
	struct efi_boot_table* boot_services;
	unsigned number_table_entries;
	void *configuration_table;
};
struct efi_system_table* _system;

struct efi_guid
{
	unsigned data1;
	unsigned data2;
};
struct efi_guid* EFI_LOADED_IMAGE_PROTOCOL_GUID;
struct efi_guid* EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID;

struct efi_loaded_image_protocol
{
	unsigned revision;
	void* parent;
	void* system;

	void* device;
	void* filepath;
	void* reserved;

	/* Image's load options */
	unsigned load_options_size;
	void* load_options;

	/* Location of the image in memory */
	void* image_base;
	unsigned image_size;
	unsigned image_code_type;
	unsigned image_data_type;
	void* unload;
};

struct efi_simple_file_system_protocol
{
	unsigned revision;
	void* open_volume;
};

struct efi_file_protocol
{
	unsigned revision;
	void* open;
	void* close;
	void* delete;
	void* read;
	void* write;
	void* get_position;
	void* set_position;
	void* get_info;
	void* set_info;
	void* flush;
	void* open_ex;
	void* read_ex;
	void* write_ex;
	void* flush_ex;
};
struct efi_file_protocol* _rootdir;

char _read(FILE* f, unsigned size, FUNCTION read)
{
	/* f->read(f, &size, &c) */
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "push !0"
	    "mov_r8,rsp"
	    "lea_rax,[rbp+DWORD] %-24"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40"
	    "lea_rax,[rbp+DWORD] %-16"
	    "mov_rax,[rax]"
	    "test_rax,rax"
	    "pop_rax"
	    "jne %_read_end"
	    "mov_rax, %0xFFFFFFFF # EOF"
	    ":_read_end");
}

long _write(FILE* f, unsigned size, char c, FUNCTION write)
{
	/* fout->write(fout, &size, &c) */
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "lea_r8,[rbp+DWORD] %-24"
	    "lea_rax,[rbp+DWORD] %-32"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40");
}

void _write_stdout(void* con_out, int c, FUNCTION output_string)
{
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "lea_rax,[rbp+DWORD] %-24"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40");
}

void* _open_protocol(void* handle, struct efi_guid* protocol, void* agent_handle, void* controller_handle, long attributes, FUNCTION open_protocol)
{
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "mov_rdx,[rdx]"
	    "push !0"
	    "mov_r8,rsp"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "lea_r9,[rbp+DWORD] %-24"
	    "mov_r9,[r9]"
	    "lea_rax,[rbp+DWORD] %-40"
	    "mov_rax,[rax]"
	    "push_rax"
	    "lea_rax,[rbp+DWORD] %-32"
	    "mov_rax,[rax]"
	    "push_rax"
	    "lea_rax,[rbp+DWORD] %-48"
	    "mov_rax,[rax]"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %56"
	    "pop_rax");
}

int _close_protocol(void *handle, struct efi_guid* protocol, void* agent_handle, void* controller_handle, FUNCTION close_protocol)
{
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "mov_rdx,[rdx]"
	    "lea_r8,[rbp+DWORD] %-24"
	    "mov_r8,[r8]"
	    "lea_r9,[rbp+DWORD] %-32"
	    "mov_r9,[r9]"
	    "lea_rax,[rbp+DWORD] %-40"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40");
}

int _open_volume(struct efi_simple_file_system_protocol* rootfs, FUNCTION open_volume)
{
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "push !0"
	    "mov_rdx,rsp"
	    "lea_rax,[rbp+DWORD] %-16"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40"
	    "pop_rax");
}

FILE* _open(void* _rootdir, char* name, long mode, long attributes, FUNCTION open)
{
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "push !0"
	    "mov_rdx,rsp"
	    "lea_r8,[rbp+DWORD] %-16"
	    "mov_r8,[r8]"
	    "lea_r9,[rbp+DWORD] %-24"
	    "mov_r9,[r9]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "push_rax"
	    "lea_rax,[rbp+DWORD] %-32"
	    "mov_rax,[rax]"
	    "push_rax"
	    "lea_rax,[rbp+DWORD] %-40"
	    "mov_rax,[rax]"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %56"
	    "pop_rax");
}

FILE* _close(FILE* f, FUNCTION close)
{
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rax,[rbp+DWORD] %-16"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40"
	);
}

/* Returns _malloc_ptr and not exit value from AllocatePages call.*/
long _allocate_pages(unsigned type, unsigned memory_type, unsigned pages, long _malloc_ptr, FUNCTION allocate_pages)
{
	/* boot->allocate_pages(EFI_ALLOCATE_ANY_PAGES, EFI_LOADER_DATA, size, &_malloc_ptr) */
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "mov_rdx,[rdx]"
	    "lea_r8,[rbp+DWORD] %-24"
	    "mov_r8,[r8]"
	    "lea_r9,[rbp+DWORD] %-32"
	    "lea_rax,[rbp+DWORD] %-40"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40"
	    "lea_rax,[rbp+DWORD] %-32"
	    "mov_rax,[rax]");
}

void _free_pages(void* memory, unsigned pages, FUNCTION free_pages)
{
	/* boot->free_pages(memory, pages) */
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "mov_rdx,[rdx]"
	    "lea_rax,[rbp+DWORD] %-24"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40");
}

int fgetc(FILE* f)
{
	struct efi_file_protocol* file = f;

	unsigned size = 1;
	char c = _read(file, size, file->read);
	return c;
}

void fputc(char c, FILE* f)
{
	unsigned size = 1;
	/* In UEFI StdErr might not be printing stuff to console, so just use stdout */
	if(f == stdout || f == stderr)
	{
		_write_stdout(_system->con_out, c, _system->con_out->output_string);
		if('\n' == c)
		{
			_write_stdout(_system->con_out, '\r', _system->con_out->output_string);
		}
		return;
	}
	struct efi_file_protocol* file = f;
	_write(file, size, c, file->write);
}

void fputs(char* s, FILE* f)
{
	while(0 != s[0])
	{
		fputc(s[0], f);
		s = s + 1;
	}
}

int strlen(char* str)
{
	int i = 0;
	while(0 != str[i]) i = i + 1;
	return i;
}

char* _posix_path_to_uefi(char *narrow_string);

FILE* fopen(char* filename, char* mode)
{
	char* wide_filename = _posix_path_to_uefi(filename);
	FILE* f;
	long status;
	if('w' == mode[0])
	{
		long mode = 1 << 63; /* EFI_FILE_MODE_CREATE = 0x8000000000000000 */
		mode = mode | EFI_FILE_MODE_WRITE | EFI_FILE_MODE_READ;
		f = _open(_rootdir, wide_filename, mode, 0, _rootdir->open);
	}
	else
	{       /* Everything else is a read */
		f = _open(_rootdir, wide_filename, EFI_FILE_MODE_READ, EFI_FILE_READ_ONLY, _rootdir->open);
	}
	return f;
}

int fclose(FILE* stream)
{
	struct efi_file_protocol* file = stream;
	return _close(file, file->close);
}

/* A very primitive memory manager */
void* malloc(int size)
{
	if(NULL == _brk_ptr)
	{
		unsigned pages = PAGE_NUM; /* 64 MiB = 16384 * 4 KiB pages */
		_malloc_ptr = _allocate_pages(EFI_ALLOCATE_ANY_PAGES, EFI_LOADER_DATA, pages, _malloc_ptr, _system->boot_services->allocate_pages);
		if(_malloc_ptr == 0)
		{
			return 0;
		}
		_brk_ptr = _malloc_ptr + pages * PAGE_SIZE;
	}

	/* We never allocate more memory in bootstrap mode */
	if(_brk_ptr < _malloc_ptr + size)
	{
		return 0;
	}

	long old_malloc = _malloc_ptr;
	_malloc_ptr = _malloc_ptr + size;
	return old_malloc;
}

void* memset(void* ptr, int value, int num)
{
	char* s;
	for(s = ptr; 0 < num; num = num - 1)
	{
		s[0] = value;
		s = s + 1;
	}
}

void* calloc(int count, int size)
{
	void* ret = malloc(count * size);
	if(NULL == ret) return NULL;
	memset(ret, 0, (count * size));
	return ret;
}

void free(void* l)
{
	return;
}

void exit(unsigned value)
{
	goto FUNCTION__exit;
}

void _posix_path_to_uefi(char *narrow_string)
{
	unsigned length = strlen(narrow_string) + 1;
	char *wide_string = calloc(length, 2);
	unsigned i;
	for(i = 0; i < length; i = i + 1)
	{
		if(narrow_string[i] == '/')
		{
			wide_string[2 * i] = '\\';
		}
		else
		{
			wide_string[2 * i] = narrow_string[i];
		}
	}
	return wide_string;
}

char* wide2string(char *wide_string, unsigned length)
{
	unsigned i;
	char *narrow_string = calloc(length, 1);
	for(i = 0; i < length; i = i + 1)
	{
		narrow_string[i] = wide_string[2 * i];
	}
	return narrow_string;
}

int is_space(char c)
{
	return (c == ' ') || (c == '\t');
}

void process_load_options(char* load_options)
{
	/* Determine argc */
	_argc = 1; /* command name */
	char *i = load_options;
	unsigned was_space = 0;
	do
	{
		if(is_space(i[0]))
		{
			if(!was_space)
			{
				_argc = _argc + 1;
				was_space = 1;
			}
		}
		else
		{
			was_space = 0;
		}
		i = i + 1;
	} while(i[0] != 0);

	/* Collect argv */
	_argv = calloc(_argc + 1, sizeof(char*));
	i = load_options;
	unsigned j;
	for(j = 0; j < _argc; j = j + 1)
	{
		_argv[j] = i;
		do
		{
			i = i + 1;
		} while(!is_space(i[0]) && i[0] != 0);
		i[0] = 0;
		do
		{
			i = i + 1;
		} while(is_space(i[0]));
	}
}

void _init()
{
	/* Allocate user stack, UEFI stack is not big enough for compilers */
	__user_stack = malloc(USER_STACK_SIZE);
	_malloc_start = __user_stack;
	/* Go to the other end of allocated memory, as stack grows downwards)*/
	__user_stack = __user_stack + USER_STACK_SIZE;

	/* Process command line arguments */
	EFI_LOADED_IMAGE_PROTOCOL_GUID = calloc(1, sizeof(struct efi_guid));
	EFI_LOADED_IMAGE_PROTOCOL_GUID->data1 = (0x11D29562 << 32) + 0x5B1B31A1;
	/* We want to add 0xA0003F8E but M2 treats 32-bit values as negatives, in order to
	 * have the same behaviour on 32-bit systems, so restrict to 31-bit constants */
	EFI_LOADED_IMAGE_PROTOCOL_GUID->data2 = (0x3B7269C9 << 32) + 0x50003F8E + 0x50000000;

	struct efi_loaded_image_protocol* image = _open_protocol(_image_handle, EFI_LOADED_IMAGE_PROTOCOL_GUID, _image_handle, 0, EFI_OPEN_PROTOCOL_BY_HANDLE_PROTOCOL, _system->boot_services->open_protocol);
	char* load_options = wide2string(image->load_options, image->load_options_size);
	process_load_options(load_options);

	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID = calloc(1, sizeof(struct efi_guid));
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID->data1 = (0x11D26459 << 32) + 0x564E5B22 + 0x40000000;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID->data2 = (0x3B7269C9 << 32) + 0x5000398E + 0x50000000;

	_root_device = image->device;
	struct efi_simple_file_system_protocol* rootfs = _open_protocol(_root_device, EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID, _image_handle, 0, EFI_OPEN_PROTOCOL_BY_HANDLE_PROTOCOL, _system->boot_services->open_protocol);
	_rootdir = _open_volume(rootfs, rootfs->open_volume);
}

void _cleanup()
{
	fclose(_rootdir);
	_close_protocol(_root_device, EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID, _image_handle, 0, _system->boot_services->close_protocol);
	_close_protocol(_image_handle, EFI_LOADED_IMAGE_PROTOCOL_GUID, _image_handle, 0, _system->boot_services->close_protocol);
	_free_pages(_malloc_start, PAGE_NUM, _system->boot_services->free_pages);
}
