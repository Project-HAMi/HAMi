/*
 * Copyright (c) 2020, NVIDIA CORPORATION. All rights reserved.
 */

#ifndef HEADER_DXCORE_H_
#define HEADER_DXCORE_H_

#define MAX_DXCORE_DRIVERSTORE_LIBRAIRIES (16)

struct dxcore_luid
{
        unsigned int lowPart;
        int highPart;
};

struct dxcore_adapter
{
        unsigned int             hAdapter;
        unsigned int             wddmVersion;
        char*                    pDriverStorePath;
        unsigned int             driverStoreComponentCount;
        const char*              pDriverStoreComponents[MAX_DXCORE_DRIVERSTORE_LIBRAIRIES];
        struct dxcore_context    *pContext;
};

struct dxcore_context
{
        unsigned int adapterCount;
        struct dxcore_adapter *adapterList;

        int initialized;
};



int dxcore_init_context(struct dxcore_context* pDxcore_context);
void dxcore_deinit_context(struct dxcore_context* pDxcore_context);

#endif // HEADER_DXCORE_H_
