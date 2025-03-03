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

#define __uefi__ 1

#ifndef _UEFI_C
#define _UEFI_C

#include <ctype.h>
#include <uefi/string_p.h>

#define PAGE_SIZE 4096
#define USER_STACK_SIZE 8388608
#define EFI_OPEN_PROTOCOL_BY_HANDLE_PROTOCOL 1
#define EFI_FILE_MODE_READ 1
#define EFI_FILE_MODE_WRITE 2
#define EFI_FILE_MODE_CREATE (1 << 63)
#define EFI_FILE_READ_ONLY 1
#define EFI_FILE_DIRECTORY 0x10
#define EFI_LOADER_DATA 2

#define EFI_VARIABLE_BOOTSERVICE_ACCESS 2

#define EFI_SUCCESS 0
#define EFI_LOAD_ERROR (1 << 63) | 1
#define EFI_INVALID_PARAMETER (1 << 63) | 2
#define EFI_UNSUPPORTED (1 << 63) | 3
#define EFI_BUFFER_TOO_SMALL (1 << 63) | 5
#define EFI_NOT_FOUND (1 << 31) | 14

#define __PATH_MAX 4096
#define __ENV_NAME_MAX 4096

#define HARDWARE_DEVICE_PATH 1
#define MEMORY_MAPPED 3
#define END_HARDWARE_DEVICE_PATH 0x7F
#define END_ENTIRE_DEVICE_PATH 0xFF

#define TPL_APPLICATION    4
#define TPL_CALLBACK       8
#define TPL_NOTIFY         16
#define TPL_HIGH_LEVEL     31

void* _image_handle;
void* _root_device;
void* __user_stack;

int _argc;
char** _argv;
char** _envp;

char* _cwd;
char* _root;

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

struct efi_runtime_table
{
	struct efi_table_header header;

	/* Time Services */
	void* get_time;
	void* set_time;
	void* get_wakeup_time;
	void* set_wakeup_time;

	/* Virtual Memory Services */
	void* set_virtual_address_map;
	void* convert_pointer;

	/* Variable Services */
	void* get_variable;
	void* get_next_variable_name;
	void* set_variable;

	/* Miscellaneous Services */
	void* get_next_high_monotonic_count;
	void* reset_system;

	/* UEFI 2.0 Capsule Services */
	void* update_capsule;
	void* query_capsule_capabilities;

	/* Miscellaneous UEFI 2.0 Services */
	void* query_variable_info;
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
	struct efi_runtime_table* runtime_services;
	struct efi_boot_table* boot_services;
	unsigned number_table_entries;
	void *configuration_table;
};
struct efi_system_table* _system;

struct efi_guid
{
	uint32_t data1;
	uint16_t data2;
	uint16_t data3;
	uint8_t data4[8];
};
struct efi_guid EFI_LOADED_IMAGE_PROTOCOL_GUID;
struct efi_guid EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID;
struct efi_guid EFI_FILE_INFO_GUID;
struct efi_guid EFI_SHELL_VARIABLE_GUID;

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

struct efi_loaded_image_protocol* _image;

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

struct efi_time
{
	uint16_t year;
	uint8_t month;
	uint8_t day;
	uint8_t hour;
	uint8_t minute;
	uint8_t second;
	uint8_t pad1;
	uint32_t nanosecond;
	uint16_t time_zone;
	uint8_t daylight;
	uint8_t pad2;
};

struct efi_file_info
{
	unsigned size;
	unsigned file_size;
	unsigned physical_size;
	struct efi_time create_time;
	struct efi_time last_access_time;
	struct efi_time modifiction_time;
	unsigned attribute;
	char file_name[__PATH_MAX];
};

struct efi_device_path_protocol
{
	uint8_t type;
	uint8_t subtype;
	uint16_t length;
	uint32_t memory_type;
	unsigned start_address;
	unsigned end_address;
};

unsigned __uefi_1(void*, void*, FUNCTION f)
{
#ifdef __x86_64__
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rax,[rbp+DWORD] %-16"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40");
#else
#error unsupported arch
#endif
}

unsigned __uefi_2(void*, void*, FUNCTION f)
{
#ifdef __x86_64__
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
#else
#error unsupported arch
#endif
}

unsigned __uefi_3(void*, void*, void*, FUNCTION f)
{
#ifdef __x86_64__
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "mov_rdx,[rdx]"
	    "lea_r8,[rbp+DWORD] %-24"
	    "mov_r8,[r8]"
	    "lea_rax,[rbp+DWORD] %-32"
	    "mov_rax,[rax]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %40");
#else
#error unsupported arch
#endif
}

unsigned __uefi_4(void*, void*, void*, void*, FUNCTION f)
{
#ifdef __x86_64__
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
#else
#error unsupported arch
#endif
}

unsigned __uefi_5(void*, void*, void*, void*, void*, FUNCTION f)
{
#ifdef __x86_64__
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "mov_rdx,[rdx]"
	    "lea_r8,[rbp+DWORD] %-24"
	    "mov_r8,[r8]"
	    "lea_r9,[rbp+DWORD] %-32"
	    "mov_r9,[r9]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "push_rax"
	    "lea_rax,[rbp+DWORD] %-40"
	    "mov_rax,[rax]"
	    "push_rax"
	    "lea_rax,[rbp+DWORD] %-48"
	    "mov_rax,[rax]"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %56");
#else
#error unsupported arch
#endif
}

unsigned __uefi_6(void*, void*, void*, void*, void*, void*, FUNCTION f)
{
#ifdef __x86_64__
	asm("lea_rcx,[rbp+DWORD] %-8"
	    "mov_rcx,[rcx]"
	    "lea_rdx,[rbp+DWORD] %-16"
	    "mov_rdx,[rdx]"
	    "lea_r8,[rbp+DWORD] %-24"
	    "mov_r8,[r8]"
	    "lea_r9,[rbp+DWORD] %-32"
	    "mov_r9,[r9]"
	    "push_rsp"
	    "push_[rsp]"
	    "and_rsp, %-16"
	    "lea_rax,[rbp+DWORD] %-48"
	    "mov_rax,[rax]"
	    "push_rax"
	    "lea_rax,[rbp+DWORD] %-40"
	    "mov_rax,[rax]"
	    "push_rax"
	    "lea_rax,[rbp+DWORD] %-56"
	    "mov_rax,[rax]"
	    "sub_rsp, %32"
	    "call_rax"
	    "mov_rsp,[rsp+BYTE] %56");
#else
#error unsupported arch
#endif
}

unsigned _allocate_pool(unsigned memory_type, unsigned size, void* pool)
{
	return __uefi_3(memory_type, size, pool, _system->boot_services->allocate_pool);
}

void _free_pool(void* memory)
{
	return __uefi_1(memory, _system->boot_services->free_pool);
}

unsigned _open_protocol(void* handle, struct efi_guid* protocol, void* agent_handle, void** interface, void* controller_handle, long attributes, FUNCTION open_protocol)
{
	return __uefi_6(handle, protocol, agent_handle, interface, controller_handle, attributes, _system->boot_services->open_protocol);
}

unsigned _close_protocol(void* handle, struct efi_guid* protocol, void* agent_handle, void* controller_handle)
{
	return __uefi_4(handle, protocol, agent_handle, controller_handle, _system->boot_services->close_protocol);
}

unsigned _open_volume(struct efi_simple_file_system_protocol* rootfs, struct efi_file_protocol** rootdir)
{
	return __uefi_2(rootfs, rootdir, rootfs->open_volume);
}

unsigned _close(struct efi_file_protocol* file)
{
	return __uefi_1(file, file->close);
}

unsigned _get_next_variable_name(unsigned* size, char* name, struct efi_guid* vendor_guid)
{
	return __uefi_3(size, name, vendor_guid, _system->runtime_services->get_next_variable_name);
}

unsigned _get_variable(char* name, struct efi_guid* vendor_guid, uint32_t* attributes, unsigned* data_size, void* data)
{
	return __uefi_5(name, vendor_guid, attributes, data_size, data, _system->runtime_services->get_variable);
}

char* _string2wide(char* narrow_string);
size_t strlen(char const* str);
void free(void* ptr);
unsigned _set_variable(char* name, void* data)
{
	char* wide_name = _string2wide(name);
	char* wide_data = _string2wide(data);
	unsigned data_size = strlen(data) * 2;
	uint32_t attributes = EFI_VARIABLE_BOOTSERVICE_ACCESS;
	unsigned rval = __uefi_5(wide_name, &EFI_SHELL_VARIABLE_GUID, attributes, data_size, wide_data, _system->runtime_services->set_variable);
	free(wide_name);
	free(wide_data);
	return rval;
}

void exit(unsigned value)
{
	goto FUNCTION__exit;
}

char* strcat(char* dest, char const* src);
char* strcpy(char* dest, char const* src);
size_t strlen(char const* str);
void* calloc(int count, int size);

char* _relative_path_to_absolute(char* narrow_string)
{
	char* absolute_path = calloc(__PATH_MAX, 1);
	if(narrow_string[0] != '/' && narrow_string[0] != '\\')
	{
		strcat(absolute_path, _cwd);
		if(_cwd[strlen(_cwd) - 1] != '/' && _cwd[strlen(_cwd) - 1] != '\\')
		{
			strcat(absolute_path, "/");
		}
	}
	else
	{
		strcat(absolute_path, _root);
        }
	strcat(absolute_path, narrow_string);

	return absolute_path;
}

char* _posix_path_to_uefi(char* narrow_string)
{
	char* absolute_path = _relative_path_to_absolute(narrow_string);

	unsigned length = strlen(absolute_path);
	unsigned in = 0;
	unsigned out = 0;
	while(in < length)
	{
		if(absolute_path[in] == '/')
		{
			absolute_path[out] = '\\';
			// Deal with /./ in paths.
			if((in < (length - 1)) && (absolute_path[in + 1] == '.') && (absolute_path[in + 2] == '/'))
			{
				in += 2;
			}
		}
		else
		{
			absolute_path[out] = absolute_path[in];
		}
		in += 1;
		out += 1;
	}
	absolute_path[out] = 0;

	char* wide_string = _string2wide(absolute_path);
	free(absolute_path);
	return wide_string;
}

char* _string2wide(char* narrow_string)
{
	unsigned length = strlen(narrow_string);
	char* wide_string = calloc(length + 1, 2);
	unsigned i;
	for(i = 0; i < length; i += 1)
	{
		wide_string[2 * i] = narrow_string[i];
	}
	return wide_string;
}

int isspace(char _c);

void _process_load_options(char* load_options)
{
	/* Determine argc */
	_argc = 1; /* command name */
	char *i = load_options;
	unsigned was_space = 0;
	do
	{
		if(isspace(i[0]))
		{
			if(!was_space)
			{
				_argc += 1;
				was_space = 1;
			}
		}
		else
		{
			was_space = 0;
		}
		i += 1;
	} while(i[0] != 0);

	/* Collect argv */
	_argv = calloc(_argc + 1, sizeof(char*));
	i = load_options;
	unsigned j;
	for(j = 0; j < _argc; j += 1)
	{
		_argv[j] = i;
		do
		{
			i += 1;
		} while(!isspace(i[0]) && i[0] != 0);
		i[0] = 0;
		do
		{
			i += 1;
		} while(isspace(i[0]));
	}
}

/* Function to find the length of a char**; an array of strings */
unsigned _array_length(char** array)
{
	unsigned length = 0;

	while(array[length] != NULL)
	{
		length += 1;
	}

	return length;
}

size_t wcstombs(char* dest, char* src, size_t n);

char* _get_environmental_variable(struct efi_guid* vendor_guid, char* name, unsigned size)
{
	unsigned data_size;
	char* data;
	char* variable_data;
	char* envp_line = NULL;

	/* Call with data=NULL to obtain data size that we need to allocate */
	_get_variable(name, vendor_guid, NULL, &data_size, NULL);
	data = calloc(data_size + 1, 1);
	_get_variable(name, vendor_guid, NULL, &data_size, data);

	variable_data = calloc((data_size / 2) + 1, 1);
	wcstombs(variable_data, data, (data_size / 2) + 1);

	envp_line = calloc((size / 2) + (data_size / 2) + 1, 1);
	wcstombs(envp_line, name, size / 2);
	strcat(envp_line, "=");
	strcat(envp_line, variable_data);
	free(data);
	free(variable_data);

	return envp_line;
}

int memcmp(void const* lhs, void const* rhs, size_t count);

char** _get_environmental_variables(char** envp)
{
	EFI_SHELL_VARIABLE_GUID.data1 = 0x158def5a;
	EFI_SHELL_VARIABLE_GUID.data2 = 0xf656;
	EFI_SHELL_VARIABLE_GUID.data3 = 0x419c;
	EFI_SHELL_VARIABLE_GUID.data4[0] = 0xb0;
	EFI_SHELL_VARIABLE_GUID.data4[1] = 0x27;
	EFI_SHELL_VARIABLE_GUID.data4[2] = 0x7a;
	EFI_SHELL_VARIABLE_GUID.data4[3] = 0x31;
	EFI_SHELL_VARIABLE_GUID.data4[4] = 0x92;
	EFI_SHELL_VARIABLE_GUID.data4[5] = 0xc0;
	EFI_SHELL_VARIABLE_GUID.data4[6] = 0x79;
	EFI_SHELL_VARIABLE_GUID.data4[7] = 0xd2;

	unsigned size = __ENV_NAME_MAX;
	unsigned rval;
	unsigned envc = 0;
	char* name = calloc(size, 1);

	struct efi_guid vendor_guid;
	/* First count the number of environmental variables */
	do
	{
		size = __ENV_NAME_MAX;
		rval = _get_next_variable_name(&size, name, &vendor_guid);
		if(rval == EFI_SUCCESS)
		{
			if(memcmp(&vendor_guid, &EFI_SHELL_VARIABLE_GUID, sizeof(struct efi_guid)) == 0)
			{
				envc += 1;
			}
		}
	} while(rval == EFI_SUCCESS);

	/* Now redo the search but this time populate envp array */
	envp = calloc(sizeof(char*), envc + 1);
	name[0] = 0;
	name[1] = 0;
	unsigned j = 0;
	do
	{
		size = __ENV_NAME_MAX;
		rval = _get_next_variable_name(&size, name, &vendor_guid);
		if(rval == EFI_SUCCESS)
		{
			if(memcmp(&vendor_guid, &EFI_SHELL_VARIABLE_GUID, sizeof(struct efi_guid)) == 0)
			{
				envp[j] = _get_environmental_variable(&vendor_guid, name, size);
				j += 1;
			}
		}
	} while(rval == EFI_SUCCESS);
	envp[j] = 0;
	free(name);

	return envp;
}

void _wipe_environment()
{
	char** envp = _get_environmental_variables(envp);
	unsigned i = 0;
	unsigned j;
	char* name;
	while(envp[i] != 0)
	{
		j = 0;
		name = envp[i];
		while(envp[i][j] != '=')
		{
			j += 1;
		}
		envp[i][j] = 0;
		_set_variable(name, "");
		i += 1;
	}
	free(envp);
}

int strcmp(char const* lhs, char const* rhs);
char* strchr(char const* str, int ch);

void _setup_current_working_directory(char** envp)
{
	_cwd = calloc(__PATH_MAX, 1);
	_root = calloc(__PATH_MAX, 1);

	unsigned i = 0;
	unsigned j;
	unsigned k;
	char* value;
	char* match;

	while(envp[i] != 0)
	{
		j = 0;
		while(envp[i][j] != '=')
		{
			j += 1;
		}
		envp[i][j] = 0;
		if(strcmp(envp[i], "root") == 0)
		{
			value = envp[i] + j + 1;
			match = strchr(value, ':'); /* strip uefi device, e.g. fs0: */
			if(match != NULL)
			{
				value = match + 1;
			}
			strcpy(_root, value);
			k = 0;
			while(_root[k] != '\0')
			{
				if(_root[k] == '\\')
				{
					_root[k] = '/';
				}
				k += 1;
			}
		}
		else if(strcmp(envp[i], "cwd") == 0)
		{
			value = envp[i] + j + 1;
			match = strchr(value, ':'); /* strip uefi device, e.g. fs0: */
			if(match != NULL)
			{
				value = match + 1;
			}
			strcpy(_cwd, value);
			k = 0;
			while(_cwd[k] != '\0')
			{
				if(_cwd[k] == '\\')
				{
					_cwd[k] = '/';
				}
				k += 1;
			}
		}
		envp[i][j] = '=';
		i += 1;
	}
	if(strcmp(_cwd, "") == 0)
	{
		strcpy(_cwd, "/");
	}
}

void* malloc(unsigned size);
void __init_io();

void _init()
{
	/* Allocate user stack, UEFI stack is not big enough for compilers */
	__user_stack = malloc(USER_STACK_SIZE) + USER_STACK_SIZE;

	/* Process command line arguments */
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data1 = 0x5b1b31a1;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data2 = 0x9562;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data3 = 0x11d2;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data4[0] = 0x8e;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data4[1] = 0x3f;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data4[2] = 0;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data4[3] = 0xa0;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data4[4] = 0xc9;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data4[5] = 0x69;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data4[6] = 0x72;
	EFI_LOADED_IMAGE_PROTOCOL_GUID.data4[7] = 0x3b;

	__init_io();
	_open_protocol(_image_handle, &EFI_LOADED_IMAGE_PROTOCOL_GUID, &_image, _image_handle, 0, EFI_OPEN_PROTOCOL_BY_HANDLE_PROTOCOL);
	char* load_options = calloc(_image->load_options_size, 1);
	wcstombs(load_options, _image->load_options, _image->load_options_size);
	_process_load_options(load_options);

	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data1 = 0x964E5B22;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data2 = 0x6459;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data3 = 0x11d2;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data4[0] = 0x8e;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data4[1] = 0x39;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data4[2] = 0;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data4[3] = 0xa0;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data4[4] = 0xc9;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data4[5] = 0x69;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data4[6] = 0x72;
	EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID.data4[7] = 0x3b;

	_root_device = _image->device;
	struct efi_simple_file_system_protocol* rootfs;
	_open_protocol(_root_device, &EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID, &rootfs, _image_handle, 0, EFI_OPEN_PROTOCOL_BY_HANDLE_PROTOCOL);
	_open_volume(rootfs, &_rootdir);

	EFI_FILE_INFO_GUID.data1 = 0x09576e92;
	EFI_FILE_INFO_GUID.data2 = 0x6d3f;
	EFI_FILE_INFO_GUID.data3 = 0x11d2;
	EFI_FILE_INFO_GUID.data4[0] = 0x8e;
	EFI_FILE_INFO_GUID.data4[1] = 0x39;
	EFI_FILE_INFO_GUID.data4[2] = 0;
	EFI_FILE_INFO_GUID.data4[3] = 0xa0;
	EFI_FILE_INFO_GUID.data4[4] = 0xc9;
	EFI_FILE_INFO_GUID.data4[5] = 0x69;
	EFI_FILE_INFO_GUID.data4[6] = 0x72;
	EFI_FILE_INFO_GUID.data4[7] = 0x3b;

	_envp = _get_environmental_variables(_envp);
	_setup_current_working_directory(_envp);
}

void __kill_io();
void* _malloc_release_all(FUNCTION _free);

void _cleanup()
{
	__kill_io();
	_close(_rootdir);
	_close_protocol(_root_device, &EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID, _image_handle, 0);
	_close_protocol(_image_handle, &EFI_LOADED_IMAGE_PROTOCOL_GUID, _image_handle, 0);
	_malloc_release_all(_free_pool);
}

void* _malloc_uefi(unsigned size)
{
	void* memory_block;
	if(_allocate_pool(EFI_LOADER_DATA, size, &memory_block) != EFI_SUCCESS)
	{
	    return 0;
	}
	return memory_block;
}

#endif
