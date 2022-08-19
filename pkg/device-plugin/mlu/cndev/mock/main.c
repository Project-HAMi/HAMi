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
#include <stdio.h>
int Test_cndevGetDeviceCount() {
	cndevCardInfo_t *cardNum;
	cardNum = (cndevCardInfo_t *)malloc(sizeof(cndevCardInfo_t));
	cardNum->version = 5;
	cndevRet_t result;
	result = cndevGetDeviceCount(cardNum);
	printf("=== Test cndevGetDeviceCount ===\nnum:%d\nret:%d\n",
	       cardNum->number, result);
	return cardNum->number;
}
void Test_cndevInit() {
	cndevRet_t result;
	result = cndevInit(0);
	printf("=== Test cndevInit ===\nret: %d\n", result);
}
void Test_cndevGetCardHealthState(int id) {
	cndevCardHealthState_t *cardHealthState;
	cardHealthState =
	    (cndevCardHealthState_t *)malloc(sizeof(cndevCardHealthState_t));
	cardHealthState->version = 5;
	cndevRet_t result;
	result = cndevGetCardHealthState(cardHealthState, id);
	printf("=== Test cndevGetCardHealthState ===\nhealth:%d\nret:%d\n",
	       cardHealthState->health, result);
}
void Test_cndevGetCardSN(int id) {
	cndevCardSN_t *cardSN;
	cardSN = (cndevCardSN_t *)malloc(sizeof(cndevCardSN_t));
	cardSN->version = 5;
	cndevRet_t result;
	result = cndevGetCardSN(cardSN, id);
	printf("=== Test cndevGetCardSN ===\nmotherBoard:%ld\nret:%d\n",
	       cardSN->motherBoardSn, result);
}

void Test_cndevGetPCIeInfo(int id) {
	cndevPCIeInfo_t *cardPcie;
	cardPcie = (cndevPCIeInfo_t *)malloc(sizeof(cndevPCIeInfo_t));
	cardPcie->version = 5;
	cndevRet_t result;
	result = cndevGetPCIeInfo(cardPcie, id);
	printf("=== Test cndevGetPcieInfo "
	       "===\ndomain:%d\nbus:%d\ndevice:%d\nfunction:%d\nret:%d\n",
	       cardPcie->domain, cardPcie->bus, cardPcie->device,
	       cardPcie->function, result);
}

void Test_cndevGetUUID(int id) {
	cndevUUID_t *uuidInfo;
	uuidInfo = (cndevUUID_t *)malloc(sizeof(cndevUUID_t));
	uuidInfo->version = 5;
	cndevRet_t result;
	result = cndevGetUUID(uuidInfo, id);
	printf("=== Test cndevGetUUID ===\nuuid:%s\nret:%d\n", uuidInfo->uuid,
	       result);
}

void Test_cndevGetCardName(int id) {
	cndevCardName_t *cardName;
	cardName = (cndevCardName_t *)malloc(sizeof(cndevCardName_t));
	cardName->version = 5;
	cndevRet_t result;
	result = cndevGetCardName(cardName, id);
	printf("=== Test cndevGetCardName ===\nid:%d\nret:%d\n", cardName->id,
	       result);
}

void Test_cndevGetMemoryUsage(int id) {
	cndevMemoryInfo_t *memInfo;
	memInfo = (cndevMemoryInfo_t *)malloc(sizeof(cndevMemoryInfo_t));
	memInfo->version = 5;
	cndevRet_t result;
	result = cndevGetMemoryUsage(memInfo, id);
	printf("=== Test cndevGetMemoryUsage ===\nid:%d\nmemory:%ld\nret:%d\n",
	       id, memInfo->physicalMemoryTotal, result);
}

void Test_cndevGetMLULinkRemoteInfo(int id) {
	cndevMLULinkRemoteInfo_t *remoteinfo;
	remoteinfo = (cndevMLULinkRemoteInfo_t *)malloc(
	    sizeof(cndevMLULinkRemoteInfo_t));
	remoteinfo->version = 5;
	cndevRet_t result;
	printf("=== Test cndevGetMLULinkRemoteInfo ===\n");
	int num;
	num = cndevGetMLULinkPortNumber(id);
	for (int i = 0; i < num; ++i) {
		result = cndevGetMLULinkRemoteInfo(remoteinfo, id, i);
		printf("port:%d remote uuid:%s, ret:%d\n", i, remoteinfo->uuid,
		       result);
	}
}

void Test_cndevGetMLULinkStatus(int id) {
	cndevMLULinkStatus_t *status;
	status = (cndevMLULinkStatus_t *)malloc(sizeof(cndevMLULinkStatus_t));
	status->version = 5;
	cndevRet_t result;
	printf("=== Test cndevGetMLULinkStatus ===\n");
	int num;
	num = cndevGetMLULinkPortNumber(id);
	for (int i = 0; i < num; ++i) {
		result = cndevGetMLULinkStatus(status, id, i);
		printf("port:%d mlulink status:%d, ret:%d\n", i,
		       status->isActive, result);
	}
}

void Test_cndevGetMLULinkPortNumber(int id) {
	int result;
	result = cndevGetMLULinkPortNumber(id);
	printf("=== Test cndevGetMLULinkPortNumber ===\nret:%d\n", result);
}

int main() {
	Test_cndevInit();
	int num;
	num = Test_cndevGetDeviceCount();
	for (int i = 0; i < num; ++i) {
		printf("================ Test card id %d =============\n", i);
		Test_cndevGetCardName(i);
		Test_cndevGetCardHealthState(i);
		Test_cndevGetCardSN(i);
		Test_cndevGetPCIeInfo(i);
		Test_cndevGetUUID(i);
		Test_cndevGetMemoryUsage(i);
		Test_cndevGetMLULinkRemoteInfo(i);
		Test_cndevGetMLULinkStatus(i);
		Test_cndevGetMLULinkPortNumber(i);

	}
	return 0;
}
