import { Entity } from "../util"

export const Home = (): JSX.Element => {

    const entityJson = localStorage.getItem("ENTITY")
    const entity = entityJson ? (JSON.parse(entityJson) as Entity) : null

    return (
        <>
            hello {entity?.ccaddr}<br />
            your role is {entity?.role}
        </>
    )

}
