/*
 * Copyright 2020 Cambricon, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include "../include/cndev.h"
#include "cJSON.h"
#include <stdio.h>
#include <stdlib.h>

cJSON *readJsonFile() {
	FILE *f;
	long len;
	char *content;
	cJSON *json;
	f = fopen(getenv("MOCK_JSON"), "rb");
	fseek(f, 0, SEEK_END);
	len = ftell(f);
	fseek(f, 0, SEEK_SET);
	content = (char *)malloc(len + 1);
	fread(content, 1, len, f);
	fclose(f);
	json = cJSON_Parse(content);
	if (!json) {
		printf("Error before: [%s]\n", cJSON_GetErrorPtr());
	}
	return json;
}
cndevRet_t cndevGetDeviceCount(cndevCardInfo_t *cardNum) {
	cJSON *config;
	unsigned numMLU;
	config = readJsonFile();
	cJSON *numObj = cJSON_GetObjectItem(config, "num");
	numMLU = numObj->valueint;
	cardNum->number = numMLU;

	cJSON_Delete(config);
	return CNDEV_SUCCESS;
}
cndevRet_t cndevInit(int reserved) { return CNDEV_SUCCESS; }
cndevRet_t cndevGetCardHealthState(cndevCardHealthState_t *cardHealthState,
				   int devId) {
	cJSON *config;
	cndevRet_t result;
	config = readJsonFile();

	cJSON *health_node = cJSON_GetObjectItem(config, "health");
	cardHealthState->health =
	    cJSON_GetArrayItem(health_node, devId)->valueint;

	cJSON_Delete(config);
	return CNDEV_SUCCESS;
}
cndevRet_t cndevGetCardSN(cndevCardSN_t *cardSN, int devId) {
	cJSON *config;
	cndevRet_t result;
	config = readJsonFile();

	cJSON *mb_sn_node = cJSON_GetObjectItem(config, "motherboard");
	cardSN->motherBoardSn = cJSON_GetArrayItem(mb_sn_node, devId)->valueint;

	cJSON_Delete(config);
	return CNDEV_SUCCESS;
}
cndevRet_t cndevRelease() { return CNDEV_SUCCESS; }
cndevRet_t cndevGetCardName(cndevCardName_t *cardName, int devId) {
	cJSON *config;
	cndevRet_t result;
	config = readJsonFile();

	cJSON *card_type_node = cJSON_GetObjectItem(config, "type");
	int card_type = cJSON_GetArrayItem(card_type_node, devId)->valueint;

	if (card_type == 0) {
		cardName->id = MLU100;
	} else if (card_type == 1) {
		cardName->id = MLU270;
	} else if (card_type == 16) {
		cardName->id = MLU220_M2;
	} else if (card_type == 17) {
		cardName->id = MLU220_EDGE;
	} else if (card_type == 18) {
		cardName->id = MLU220_EVB;
	} else if (card_type == 19) {
		cardName->id = MLU220_M2i;
	} else if (card_type == 20) {
		cardName->id = MLU290;
	} else if (card_type == 23) {
		cardName->id = MLU370;
	}

	cJSON_Delete(config);
	return CNDEV_SUCCESS;
}

const char *getCardNameStringByDevId(int devId) {
	cJSON *config;
	cndevRet_t result;
	config = readJsonFile();

	cJSON *card_type_array = cJSON_GetObjectItem(config, "type");
	int card_type = cJSON_GetArrayItem(card_type_array, devId)->valueint;

	cJSON_Delete(config);

	if (card_type == 0) {
		return "MLU100";
	} else if (card_type == 1) {
		return "MLU270";
	} else if (card_type == 16 || card_type == 17 || card_type == 18 ||
		   card_type == 19) {
		return "MLU220";
	} else if (card_type == 20) {
		return "MLU290";
	} else if (card_type == 23) {
		return "MLU370";
	}
}

cndevRet_t cndevGetUUID(cndevUUID_t *uuidInfo, int devId) {
	cJSON *config;
	cndevRet_t result;
	config = readJsonFile();

	cJSON *uuid_info = cJSON_GetObjectItem(config, "uuid");
	cJSON *uuid = cJSON_GetArrayItem(uuid_info, devId);
	for (int i = 0; i < UUID_SIZE; ++i) {
		uuidInfo->uuid[i] = cJSON_GetArrayItem(uuid, i)->valueint;
	}
	cJSON_Delete(config);
	return CNDEV_SUCCESS;
}

const char *cndevGetErrorString(cndevRet_t errorId) {
	return "mock return value of cndev get error string";
}

cndevRet_t cndevGetPCIeInfo(cndevPCIeInfo_t *deviceInfo, int devId) {
	cJSON *config;
	cndevRet_t result;
	config = readJsonFile();

	cJSON *pcie_info = cJSON_GetObjectItem(config, "pcie_info");
	cJSON *pcie_node = cJSON_GetArrayItem(pcie_info, devId);

	deviceInfo->domain = cJSON_GetArrayItem(pcie_node, 0)->valueint;
	deviceInfo->bus = cJSON_GetArrayItem(pcie_node, 1)->valueint;
	deviceInfo->device = cJSON_GetArrayItem(pcie_node, 2)->valueint;
	deviceInfo->function = cJSON_GetArrayItem(pcie_node, 3)->valueint;

	cJSON_Delete(config);

	return CNDEV_SUCCESS;
}

cndevRet_t cndevGetMemoryUsage(cndevMemoryInfo_t *memInfo, int devId) {
	cJSON *config;
	cndevRet_t result;
	__int64_t memory;
	config = readJsonFile();

	cJSON *memoryObj = cJSON_GetObjectItem(config, "memory");
	memory = memoryObj->valueint;
	memInfo->physicalMemoryTotal = memory;

	cJSON_Delete(config);
	return CNDEV_SUCCESS;
}

cndevRet_t cndevGetMLULinkRemoteInfo(cndevMLULinkRemoteInfo_t *remoteinfo,
				     int devId, int link) {
	cJSON *config;
	cndevRet_t result;
	config = readJsonFile();

	cJSON *remote_info = cJSON_GetObjectItem(config, "remote_info");
	cJSON *info_array = cJSON_GetArrayItem(remote_info, devId);
	cJSON *link_info = cJSON_GetArrayItem(info_array, link);

	for (int i = 0; i < UUID_SIZE; ++i) {
		remoteinfo->uuid[i] =
		    cJSON_GetArrayItem(link_info, i)->valueint;
	}
	cJSON_Delete(config);
	return CNDEV_SUCCESS;
}

cndevRet_t cndevGetMLULinkStatus(cndevMLULinkStatus_t *status, int devId,
				 int link) {
	cJSON *config;
	cndevRet_t result;
	config = readJsonFile();

	cJSON *mlulink_status_array =
	    cJSON_GetObjectItem(config, "mlulink_status");
	cJSON *dev_info = cJSON_GetArrayItem(mlulink_status_array, devId);
	status->isActive = cJSON_GetArrayItem(dev_info, link)->valueint;
	cJSON_Delete(config);
	return CNDEV_SUCCESS;
}


int cndevGetMLULinkPortNumber(int devId) {
	cJSON *config;
	int result;
	config = readJsonFile();

	return cJSON_GetObjectItem(config, "mlulink_port")->valueint;
}
