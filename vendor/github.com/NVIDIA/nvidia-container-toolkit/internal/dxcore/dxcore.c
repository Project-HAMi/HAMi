/*
 * Copyright (c) 2020, NVIDIA CORPORATION. All rights reserved.
 */

#include <dlfcn.h>
#include <stdlib.h>

#include "dxcore.h"

// We define log_write as an empty macro to allow dxcore to remain unchanged.
#define log_write(...)

// We define the following macros to allow dxcore to remain largely unchanged.
#define log_info(msg) log_write('I', __FILE__, __LINE__, msg)
#define log_warn(msg) log_write('W', __FILE__, __LINE__, msg)
#define log_err(msg)  log_write('E', __FILE__, __LINE__, msg)
#define log_infof(fmt, ...) log_write('I', __FILE__, __LINE__, fmt, __VA_ARGS__)
#define log_warnf(fmt, ...) log_write('W', __FILE__, __LINE__, fmt, __VA_ARGS__)
#define log_errf(fmt, ...)  log_write('E', __FILE__, __LINE__, fmt, __VA_ARGS__)


#define DXCORE_MAX_PATH 260

/*
 * List of components we expect to find in the driver store that we need to mount
 */
static const char * const dxcore_nvidia_driver_store_components[] = {
        "libcuda.so.1.1",                   /* Core library for cuda support */
        "libcuda_loader.so",                /* Core library for cuda support on WSL */
        "libnvidia-ptxjitcompiler.so.1",    /* Core library for PTX Jit support */
        "libnvidia-ml.so.1",                /* Core library for nvml */
        "libnvidia-ml_loader.so",           /* Core library for nvml on WSL */
        "nvidia-smi",                       /* nvidia-smi binary*/
        "nvcubins.bin",                     /* Binary containing GPU code for cuda */
};


/*
 * List of functions and structures we need to communicate with libdxcore.
 * Documentation on these functions can be found on docs.microsoft.com in d3dkmthk.
 */

struct dxcore_enumAdapters2;
struct dxcore_queryAdapterInfo;

typedef int(*pfnDxcoreEnumAdapters2)(struct dxcore_enumAdapters2* pParams);
typedef int(*pfnDxcoreQueryAdapterInfo)(struct dxcore_queryAdapterInfo* pParams);

struct dxcore_lib {
        void* hDxcoreLib;
        pfnDxcoreEnumAdapters2 pDxcoreEnumAdapters2;
        pfnDxcoreQueryAdapterInfo pDxcoreQueryAdapterInfo;
};

struct dxcore_adapterInfo
{
        unsigned int              hAdapter;
        struct dxcore_luid        AdapterLuid;
        unsigned int              NumOfSources;
        unsigned int              bPresentMoveRegionsPreferred;
};

struct dxcore_enumAdapters2
{
        unsigned int                   NumAdapters;
        struct dxcore_adapterInfo     *pAdapters;
};

enum dxcore_kmtqueryAdapterInfoType
{
        DXCORE_QUERYDRIVERVERSION = 13,
        DXCORE_QUERYREGISTRY = 48,
};

enum dxcore_queryregistry_type {
        DXCORE_QUERYREGISTRY_DRIVERSTOREPATH = 2,
        DXCORE_QUERYREGISTRY_DRIVERIMAGEPATH = 3,
};

enum dxcore_queryregistry_status {
        DXCORE_QUERYREGISTRY_STATUS_SUCCESS = 0,
        DXCORE_QUERYREGISTRY_STATUS_BUFFER_OVERFLOW = 1,
        DXCORE_QUERYREGISTRY_STATUS_FAIL = 2,
};

struct dxcore_queryregistry_info {
        enum dxcore_queryregistry_type        QueryType;
        unsigned int                          QueryFlags;
        wchar_t                               ValueName[DXCORE_MAX_PATH];
        unsigned int                          ValueType;
        unsigned int                          PhysicalAdapterIndex;
        unsigned int                          OutputValueSize;
        enum dxcore_queryregistry_status      Status;
        union {
                unsigned long long                    OutputQword;
                wchar_t                               Output;
        };
};

struct dxcore_queryAdapterInfo
{
        unsigned int                           hAdapter;
        enum dxcore_kmtqueryAdapterInfoType    Type;
        void                                   *pPrivateDriverData;
        unsigned int                           PrivateDriverDataSize;
};

static int dxcore_query_adapter_info_helper(struct dxcore_lib* pLib,
                                            unsigned int hAdapter,
                                            enum dxcore_kmtqueryAdapterInfoType type,
                                            void* pPrivateDriverDate,
                                            unsigned int privateDriverDataSize)
{
        struct dxcore_queryAdapterInfo queryAdapterInfo = { 0 };

        queryAdapterInfo.hAdapter = hAdapter;
        queryAdapterInfo.Type = type;
        queryAdapterInfo.pPrivateDriverData = pPrivateDriverDate;
        queryAdapterInfo.PrivateDriverDataSize = privateDriverDataSize;

        return pLib->pDxcoreQueryAdapterInfo(&queryAdapterInfo);
}

static int dxcore_query_adapter_wddm_version(struct dxcore_lib* pLib, unsigned int hAdapter, unsigned int* version)
{
        return dxcore_query_adapter_info_helper(pLib,
                                                hAdapter,
                                                DXCORE_QUERYDRIVERVERSION,
                                                (void*)version,
                                                sizeof(*version));
}

static int dxcore_query_adapter_driverstore(struct dxcore_lib* pLib, unsigned int hAdapter, char** ppDriverStorePath)
{
        struct dxcore_queryregistry_info params = {0};
        struct dxcore_queryregistry_info* pValue = NULL;
        wchar_t* pOutput;
        size_t outputSizeInBytes;
        size_t outputSize;

        params.QueryType = DXCORE_QUERYREGISTRY_DRIVERSTOREPATH;

        if (dxcore_query_adapter_info_helper(pLib,
                                             hAdapter,
                                             DXCORE_QUERYREGISTRY,
                                             (void*)&params,
                                             sizeof(params)))
        {
                log_err("Failed to query driver store path size for the WDDM Adapter");
                return (-1);
        }

        if (params.OutputValueSize > DXCORE_MAX_PATH * sizeof(wchar_t)) {
                log_err("The driver store path size returned by dxcore is not valid");
                return (-1);
        }

        outputSizeInBytes = (size_t)params.OutputValueSize;
        outputSize = outputSizeInBytes / sizeof(wchar_t);

        pValue = calloc(sizeof(struct dxcore_queryregistry_info) + outputSizeInBytes + sizeof(wchar_t), 1);
        if (!pValue) {
                log_err("Out of memory while allocating temp buffer to query adapter info");
                return (-1);
        }

        pValue->QueryType = DXCORE_QUERYREGISTRY_DRIVERSTOREPATH;
        pValue->OutputValueSize = (unsigned int)outputSizeInBytes;

        if (dxcore_query_adapter_info_helper(pLib,
                                            hAdapter,
                                            DXCORE_QUERYREGISTRY,
                                            (void*)pValue,
                                            (unsigned int)(sizeof(struct dxcore_queryregistry_info) + outputSizeInBytes)))
        {
                log_err("Failed to query driver store path data for the WDDM Adapter");
                free(pValue);
                return (-1);
        }
        pOutput = (wchar_t*)(&pValue->Output);

        // Make sure no matter what happened the wchar_t string is null terminated
        pOutput[outputSize] = L'\0';

        // Convert the output into a regular c string
        *ppDriverStorePath = (char*)calloc(outputSize + 1, sizeof(char));
        if (!*ppDriverStorePath) {
                log_err("Out of memory while allocating the buffer for the driver store path");
                free(pValue);
                return (-1);
        }
        wcstombs(*ppDriverStorePath, pOutput, outputSize);

        free(pValue);

    return 0;
}

static void dxcore_add_adapter(struct dxcore_context* pCtx, struct dxcore_lib* pLib, struct dxcore_adapterInfo *pAdapterInfo)
{
        unsigned int wddmVersion = 0;
        char* driverStorePath = NULL;

        log_infof("Creating a new WDDM Adapter for hAdapter:%x luid:%llx", pAdapterInfo->hAdapter, *((unsigned long long*)&pAdapterInfo->AdapterLuid));

        if (dxcore_query_adapter_wddm_version(pLib, pAdapterInfo->hAdapter, &wddmVersion)) {
                log_err("Failed to query the WDDM version for the specified adapter. Skipping it.");
                return;
        }

        if (wddmVersion < 2700) {
                log_err("Found a WDDM adapter running a driver with pre-WDDM 2.7 . Skipping it.");
                return;
        }

        if (dxcore_query_adapter_driverstore(pLib, pAdapterInfo->hAdapter, &driverStorePath)) {
                log_err("Failed to query driver store path for the WDDM Adapter . Skipping it.");
                return;
        }

        // We got all the info we needed. Adding it to the tracking structure.
        {
                struct dxcore_adapter* newList;
                newList = realloc(pCtx->adapterList, sizeof(struct dxcore_adapter) * (pCtx->adapterCount + 1));
                if (!newList) {
                        log_err("Out of memory when trying to add a new WDDM Adapter to the list of valid adapters");
                        free(driverStorePath);
                        return;
                }

                pCtx->adapterList = newList;

                pCtx->adapterList[pCtx->adapterCount].hAdapter = pAdapterInfo->hAdapter;
                pCtx->adapterList[pCtx->adapterCount].pDriverStorePath = driverStorePath;
                pCtx->adapterList[pCtx->adapterCount].wddmVersion = wddmVersion;
                pCtx->adapterCount++;
        }

        log_infof("Adding new adapter via dxcore hAdapter:%x luid:%llx wddm version:%d", pAdapterInfo->hAdapter, *((unsigned long long*)&pAdapterInfo->AdapterLuid), wddmVersion);
}

static void dxcore_enum_adapters(struct dxcore_context* pCtx, struct dxcore_lib* pLib)
{
        struct dxcore_enumAdapters2 params = {0};
        unsigned int adapterIndex = 0;

        params.NumAdapters = 0;
        params.pAdapters = NULL;

        if (pLib->pDxcoreEnumAdapters2(&params)) {
                log_err("Failed to enumerate adapters via dxcore");
                return;
        }

        params.pAdapters = malloc(sizeof(struct dxcore_adapterInfo) * params.NumAdapters);
        if (pLib->pDxcoreEnumAdapters2(&params)) {
                free(params.pAdapters);
                log_err("Failed to enumerate adapters via dxcore");
                return;
        }

        for (adapterIndex = 0; adapterIndex < params.NumAdapters; adapterIndex++) {
                dxcore_add_adapter(pCtx, pLib, &params.pAdapters[adapterIndex]);
        }

        free(params.pAdapters);
}

int dxcore_init_context(struct dxcore_context* pCtx)
{
        struct dxcore_lib lib = {0};

        pCtx->initialized = 0;
        pCtx->adapterCount = 0;
        pCtx->adapterList = NULL;

        lib.hDxcoreLib = dlopen("libdxcore.so", RTLD_LAZY);
        if (!lib.hDxcoreLib) {
                goto error;
        }

        lib.pDxcoreEnumAdapters2 = (pfnDxcoreEnumAdapters2)dlsym(lib.hDxcoreLib, "D3DKMTEnumAdapters2");
        if (!lib.pDxcoreEnumAdapters2) {
                log_err("dxcore library is present but the symbol D3DKMTEnumAdapters2 is missing");
                goto error;
        }

        lib.pDxcoreQueryAdapterInfo = (pfnDxcoreQueryAdapterInfo)dlsym(lib.hDxcoreLib, "D3DKMTQueryAdapterInfo");
        if (!lib.pDxcoreQueryAdapterInfo) {
                log_err("dxcore library is present but the symbol D3DKMTQueryAdapterInfo is missing");
                goto error;
        }

        dxcore_enum_adapters(pCtx, &lib);

        log_info("dxcore layer initialized successfully");
        pCtx->initialized = 1;

        dlclose(lib.hDxcoreLib);

        return 0;

error:
        dxcore_deinit_context(pCtx);

        if (lib.hDxcoreLib)
                dlclose(lib.hDxcoreLib);

        return (-1);
}

static void dxcore_deinit_adapter(struct dxcore_adapter* pAdapter)
{
        if (!pAdapter)
            return;

        free(pAdapter->pDriverStorePath);
}

void dxcore_deinit_context(struct dxcore_context* pCtx)
{
        unsigned int adapterIndex = 0;

        if (!pCtx)
                return;

        for (adapterIndex = 0; adapterIndex < pCtx->adapterCount; adapterIndex++) {
                dxcore_deinit_adapter(&pCtx->adapterList[adapterIndex]);
        }

        free(pCtx->adapterList);

        pCtx->initialized = 0;
}
