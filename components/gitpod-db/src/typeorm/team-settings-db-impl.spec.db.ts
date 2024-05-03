/**
 * Copyright (c) 2022 Gitpod GmbH. All rights reserved.
 * Licensed under the GNU Affero General Public License (AGPL).
 * See License.AGPL.txt in the project root for license information.
 */

import { suite, test, timeout } from "@testdeck/mocha";
import * as chai from "chai";
import { TeamDB } from "../team-db";
import { testContainer } from "../test-container";
import { resetDB } from "../test/reset-db";
import { UserDB } from "../user-db";
import { TypeORM } from "./typeorm";
import { OrganizationSettings } from "@gitpod/gitpod-protocol";
const expect = chai.expect;

@suite(timeout(10000))
export class TeamSettingsDBSpec {
    private readonly teamDB = testContainer.get<TeamDB>(TeamDB);
    private readonly userDB = testContainer.get<UserDB>(UserDB);

    async before() {
        await this.wipeRepo();
    }

    async after() {
        await this.wipeRepo();
    }

    async wipeRepo() {
        const typeorm = testContainer.get<TypeORM>(TypeORM);
        await resetDB(typeorm);
    }

    @test()
    async testPersist(): Promise<void> {
        const user = await this.userDB.newUser();
        const org = await this.teamDB.createTeam(user.id, "test team");

        const settings: Partial<OrganizationSettings> = {
            defaultRole: undefined,
        };
        const actualSettings = await this.teamDB.setOrgSettings(org.id, settings);
        delete (actualSettings as any).orgId; // seems we get that from the DB as well...
        expect(JSON.stringify(actualSettings, undefined, 2)).to.equal(JSON.stringify(settings, undefined, 2));
    }
}
